"""Async batch task model + optional Redis snapshot persistence."""

import asyncio
import json
import time
import uuid
from typing import Any, Dict, List, Optional, Protocol


class TaskSnapshotStore(Protocol):
    """Persist task snapshots outside the current process."""

    def publish(self, task: "AsyncTask", event: Dict[str, Any] | None = None) -> None:
        """Persist the latest task state without blocking the caller."""


class RedisTaskSnapshotStore:
    """Redis-backed task snapshot store.

    The live SSE fan-out remains process-local. Redis stores durable snapshots so
    other workers can inspect task state and completed results.
    """

    def __init__(
        self,
        redis: Any,
        *,
        key_prefix: str = "runtime:task:",
        ttl_s: int = 300,
    ) -> None:
        self._redis = redis
        self._key_prefix = key_prefix
        self._ttl_s = max(1, int(ttl_s))
        self._pending: set[asyncio.Task] = set()

    def publish(self, task: "AsyncTask", event: Dict[str, Any] | None = None) -> None:
        try:
            loop = asyncio.get_running_loop()
        except RuntimeError:
            return
        pending = loop.create_task(self._write(task, event))
        self._pending.add(pending)
        pending.add_done_callback(self._pending.discard)

    async def _write(self, task: "AsyncTask", event: Dict[str, Any] | None) -> None:
        try:
            key = f"{self._key_prefix}{task.id}"
            mapping = {
                "snapshot": json.dumps(task.snapshot(), separators=(",", ":")),
                "updated_at": str(time.time()),
            }
            final = task.final_event()
            if final:
                mapping["final_event"] = json.dumps(final, separators=(",", ":"))
            elif event:
                mapping["last_event"] = json.dumps(event, separators=(",", ":"))
            await self._redis.hset(key, mapping=mapping)
            await self._redis.expire(key, self._ttl_s)
        except Exception:
            return

    async def flush(self) -> None:
        if self._pending:
            await asyncio.gather(*list(self._pending))

    async def get_snapshot(self, task_id: str) -> Dict[str, Any] | None:
        raw = await self._redis.hgetall(f"{self._key_prefix}{task_id}")
        if not raw:
            return None

        def _s(value: Any) -> str:
            return value.decode() if isinstance(value, bytes) else str(value)

        snapshot = raw.get("snapshot") or raw.get(b"snapshot")
        return json.loads(_s(snapshot)) if snapshot else None


class AsyncTask:
    """Tracks progress of an async batch operation with fan-out SSE support."""

    __slots__ = (
        "id", "total", "processed", "ok", "fail", "status",
        "warning", "result", "error", "created_at",
        "cancelled", "_queues", "_final_event", "_snapshot_store",
    )

    def __init__(
        self,
        total: int,
        *,
        snapshot_store: TaskSnapshotStore | None = None,
    ) -> None:
        self.id = uuid.uuid4().hex
        self.total = int(total)
        self.processed = 0
        self.ok = 0
        self.fail = 0
        self.status = "running"
        self.warning: Optional[str] = None
        self.result: Optional[Dict[str, Any]] = None
        self.error: Optional[str] = None
        self.created_at = time.time()
        self.cancelled = False
        self._queues: List[asyncio.Queue] = []
        self._final_event: Optional[Dict[str, Any]] = None
        self._snapshot_store = snapshot_store

    # -- Fan-out pub/sub ---------------------------------------------------

    def _publish(self, event: Dict[str, Any]) -> None:
        for q in list(self._queues):
            try:
                q.put_nowait(event)
            except Exception:
                pass
        if self._snapshot_store is not None:
            self._snapshot_store.publish(self, event)

    def attach(self) -> asyncio.Queue:
        q: asyncio.Queue = asyncio.Queue(maxsize=200)
        self._queues.append(q)
        return q

    def detach(self, q: asyncio.Queue) -> None:
        if q in self._queues:
            self._queues.remove(q)

    # -- Recording ---------------------------------------------------------

    def record(
        self,
        success: bool,
        *,
        item: Any = None,
        detail: Any = None,
        error: str = "",
        count: int = 1,
    ) -> None:
        delta = max(1, int(count))
        self.processed += delta
        if success:
            self.ok += delta
        else:
            self.fail += delta
        event: Dict[str, Any] = {
            "type": "progress",
            "task_id": self.id,
            "total": self.total,
            "processed": self.processed,
            "ok": self.ok,
            "fail": self.fail,
        }
        if item is not None:
            event["item"] = item
        if detail is not None:
            event["detail"] = detail
        if error:
            event["error"] = error
        self._publish(event)

    def finish(self, result: Dict[str, Any], *, warning: Optional[str] = None) -> None:
        self.status = "done"
        self.result = result
        self.warning = warning
        event = {
            "type": "done",
            "task_id": self.id,
            "total": self.total,
            "processed": self.processed,
            "ok": self.ok,
            "fail": self.fail,
            "warning": self.warning,
            "result": result,
        }
        self._final_event = event
        self._publish(event)

    def fail_task(self, msg: str) -> None:
        self.status = "error"
        self.error = msg
        event = {
            "type": "error",
            "task_id": self.id,
            "total": self.total,
            "processed": self.processed,
            "ok": self.ok,
            "fail": self.fail,
            "error": msg,
        }
        self._final_event = event
        self._publish(event)

    def cancel(self) -> None:
        self.cancelled = True

    def finish_cancelled(self) -> None:
        self.status = "cancelled"
        event = {
            "type": "cancelled",
            "task_id": self.id,
            "total": self.total,
            "processed": self.processed,
            "ok": self.ok,
            "fail": self.fail,
        }
        self._final_event = event
        self._publish(event)

    # -- Snapshots ---------------------------------------------------------

    def snapshot(self) -> Dict[str, Any]:
        return {
            "task_id": self.id,
            "status": self.status,
            "total": self.total,
            "processed": self.processed,
            "ok": self.ok,
            "fail": self.fail,
            "warning": self.warning,
        }

    def final_event(self) -> Optional[Dict[str, Any]]:
        return self._final_event


# ---------------------------------------------------------------------------
# In-memory task store
# ---------------------------------------------------------------------------

_TASKS: Dict[str, AsyncTask] = {}
_SNAPSHOT_STORE: TaskSnapshotStore | None = None


def set_task_snapshot_store(store: TaskSnapshotStore | None) -> None:
    global _SNAPSHOT_STORE
    _SNAPSHOT_STORE = store


def create_task(total: int) -> AsyncTask:
    task = AsyncTask(total, snapshot_store=_SNAPSHOT_STORE)
    _TASKS[task.id] = task
    if _SNAPSHOT_STORE is not None:
        _SNAPSHOT_STORE.publish(task)
    return task


def get_task(task_id: str) -> Optional[AsyncTask]:
    return _TASKS.get(task_id)


async def expire_task(task_id: str, ttl_s: int = 300) -> None:
    await asyncio.sleep(ttl_s)
    _TASKS.pop(task_id, None)


__all__ = [
    "AsyncTask",
    "RedisTaskSnapshotStore",
    "TaskSnapshotStore",
    "create_task",
    "get_task",
    "expire_task",
    "set_task_snapshot_store",
]
