"""Redis-backed runtime primitives.

These helpers use Redis as an optional coordination layer. They are deliberately
small: callers keep their local hot path and opt into Redis for cross-process
state such as locks, counters, task snapshots, and short-lived cache entries.
"""

from __future__ import annotations

import os
import socket
import uuid
from typing import Any


def _decode(value: Any) -> str:
    return value.decode() if isinstance(value, bytes) else str(value)


class RedisRuntimeLease:
    """A named Redis lock lease."""

    def __init__(self, redis: Any, key: str, owner: str, ttl_ms: int) -> None:
        self._redis = redis
        self.key = key
        self.owner = owner
        self.ttl_ms = max(1, int(ttl_ms))
        self._released = False

    async def renew(self) -> bool:
        if self._released:
            return False
        current = await self._redis.get(self.key)
        if current is None or _decode(current) != self.owner:
            return False
        await self._redis.expire(self.key, max(1, self.ttl_ms // 1000))
        return True

    async def release(self) -> bool:
        if self._released:
            return False
        current = await self._redis.get(self.key)
        if current is None or _decode(current) != self.owner:
            self._released = True
            return False
        await self._redis.delete(self.key)
        self._released = True
        return True


class RedisRuntimeStore:
    """Small Redis runtime facade for locks and future shared state."""

    def __init__(self, redis: Any, *, key_prefix: str = "runtime:") -> None:
        self.redis = redis
        self.key_prefix = key_prefix

    def key(self, *parts: str) -> str:
        safe = ":".join(str(part).strip(":") for part in parts if str(part))
        return f"{self.key_prefix}{safe}"

    async def acquire_lock(
        self,
        name: str,
        *,
        owner: str | None = None,
        ttl_ms: int = 300_000,
    ) -> RedisRuntimeLease | None:
        owner = owner or f"{socket.gethostname()}:{os.getpid()}:{uuid.uuid4().hex}"
        key = self.key("lock", name)
        acquired = await self.redis.set(key, owner, nx=True, px=max(1, int(ttl_ms)))
        if not acquired:
            return None
        return RedisRuntimeLease(self.redis, key, owner, ttl_ms)

    async def close(self) -> None:
        close = getattr(self.redis, "aclose", None)
        if close is not None:
            await close()


def runtime_redis_url() -> str:
    return (os.getenv("RUNTIME_REDIS_URL") or os.getenv("ACCOUNT_REDIS_URL") or "").strip()


def create_runtime_store_from_env() -> RedisRuntimeStore | None:
    url = runtime_redis_url()
    if not url:
        return None
    from redis.asyncio import Redis

    return RedisRuntimeStore(Redis.from_url(url, decode_responses=False))


__all__ = [
    "RedisRuntimeLease",
    "RedisRuntimeStore",
    "create_runtime_store_from_env",
    "runtime_redis_url",
]
