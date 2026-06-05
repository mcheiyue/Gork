import asyncio
import json
import unittest

from app.control.account.backends.redis import RedisAccountRepository
from app.control.account.commands import AccountPatch, AccountUpsert, ListAccountsQuery
from app.control.account.models import AccountRecord
from app.control.account.quota_defaults import default_quota_set
from app.platform.runtime.redis_runtime import RedisRuntimeStore
from app.platform.runtime.task import AsyncTask, RedisTaskSnapshotStore


class _FakeRedis:
    def __init__(self, *, fail_scan: bool = False) -> None:
        self.strings: dict[str, str] = {}
        self.hashes: dict[str, dict[str, str]] = {}
        self.sets: dict[str, set[str]] = {}
        self.zsets: dict[str, dict[str, float]] = {}
        self.fail_scan = fail_scan
        self.ttl: dict[str, int] = {}

    async def setnx(self, key, value):
        if key in self.strings:
            return False
        self.strings[key] = str(value)
        return True

    async def set(self, key, value, *, nx=False, px=None):
        if nx and key in self.strings:
            return False
        self.strings[key] = str(value)
        if px is not None:
            self.ttl[key] = int(px)
        return True

    async def get(self, key):
        return self.strings.get(key)

    async def incr(self, key):
        value = int(self.strings.get(key, "0")) + 1
        self.strings[key] = str(value)
        return value

    async def hset(self, key, mapping=None, **_kwargs):
        self.hashes.setdefault(key, {}).update({str(k): str(v) for k, v in (mapping or {}).items()})
        return len(mapping or {})

    async def hget(self, key, field):
        return self.hashes.get(key, {}).get(str(field))

    async def hgetall(self, key):
        return dict(self.hashes.get(key, {}))

    async def sadd(self, key, *values):
        target = self.sets.setdefault(key, set())
        before = len(target)
        target.update(str(v) for v in values)
        return len(target) - before

    async def srem(self, key, *values):
        target = self.sets.setdefault(key, set())
        before = len(target)
        for value in values:
            target.discard(str(value))
        return before - len(target)

    async def smembers(self, key):
        return set(self.sets.get(key, set()))

    async def zadd(self, key, mapping):
        self.zsets.setdefault(key, {}).update({str(k): float(v) for k, v in mapping.items()})
        return len(mapping)

    async def zrem(self, key, *values):
        target = self.zsets.setdefault(key, {})
        before = len(target)
        for value in values:
            target.pop(str(value), None)
        return before - len(target)

    async def zrangebyscore(self, key, min_score, max_score, *, withscores=False, start=None, num=None):
        max_value = float("inf") if max_score == "+inf" else float(max_score)
        rows = [
            (member, score)
            for member, score in self.zsets.get(key, {}).items()
            if float(min_score) <= score <= max_value
        ]
        rows.sort(key=lambda item: item[1])
        if start is not None and num is not None:
            rows = rows[start : start + num]
        return rows if withscores else [member for member, _score in rows]

    async def scan_iter(self, *_args, **_kwargs):
        if self.fail_scan:
            raise AssertionError("list_accounts should use Redis indexes instead of scan_iter")
        for key in list(self.hashes):
            if key.startswith("accounts:record:"):
                yield key

    async def expire(self, key, ttl):
        self.ttl[key] = int(ttl)
        return True

    async def delete(self, key):
        removed = 0
        for store in (self.strings, self.hashes, self.sets, self.zsets):
            if key in store:
                removed += 1
                store.pop(key, None)
        return removed

    async def aclose(self):
        return None


class RedisRuntimeFeatureTests(unittest.IsolatedAsyncioTestCase):
    async def test_redis_runtime_store_acquires_and_releases_named_lock(self):
        redis = _FakeRedis()
        store = RedisRuntimeStore(redis, key_prefix="runtime:")

        first = await store.acquire_lock("scheduler", owner="worker-a", ttl_ms=30_000)
        second = await store.acquire_lock("scheduler", owner="worker-b", ttl_ms=30_000)

        self.assertIsNotNone(first)
        self.assertIsNone(second)
        await first.release()
        third = await store.acquire_lock("scheduler", owner="worker-b", ttl_ms=30_000)
        self.assertIsNotNone(third)

    async def test_async_task_persists_snapshots_to_redis_store(self):
        redis = _FakeRedis()
        store = RedisTaskSnapshotStore(redis, ttl_s=60)
        task = AsyncTask(total=3, snapshot_store=store)

        task.record(True, count=2)
        task.finish({"summary": {"total": 3, "ok": 2, "fail": 0}})
        await store.flush()

        raw = redis.hashes[f"runtime:task:{task.id}"]
        snapshot = json.loads(raw["snapshot"])
        final_event = json.loads(raw["final_event"])

        self.assertEqual("done", snapshot["status"])
        self.assertEqual(2, snapshot["processed"])
        self.assertEqual("done", final_event["type"])
        self.assertEqual(60, redis.ttl[f"runtime:task:{task.id}"])

    async def test_redis_account_listing_uses_indexes_without_scanning_records(self):
        redis = _FakeRedis(fail_scan=True)
        repo = RedisAccountRepository(redis)
        await repo.initialize()
        await repo.upsert_accounts(
            [
                AccountUpsert(token="tok-001", pool="basic", tags=["nsfw"]),
                AccountUpsert(token="tok-002", pool="basic", tags=[]),
                AccountUpsert(token="tok-003", pool="super", tags=["nsfw"]),
            ]
        )
        await repo.patch_accounts([AccountPatch(token="tok-002", status="disabled")])

        page = await repo.list_accounts(
            ListAccountsQuery(
                tags=["nsfw"],
                page=1,
                page_size=10,
                sort_by="token",
                sort_desc=False,
            )
        )

        self.assertEqual(2, page.total)
        self.assertEqual(["tok-001", "tok-003"], [item.token for item in page.items])

    async def test_redis_initialize_rebuilds_missing_indexes_once_for_existing_records(self):
        redis = _FakeRedis()
        redis.strings["accounts:rev"] = "1"
        repo = RedisAccountRepository(redis)
        old_record = AccountRecord(
            token="tok-old",
            pool="basic",
            tags=["legacy"],
            quota=default_quota_set("basic").to_dict(),
            created_at=1,
            updated_at=1,
        )
        redis.hashes["accounts:record:tok-old"] = repo._to_hash(old_record, 1)

        await repo.initialize()
        redis.fail_scan = True
        page = await repo.list_accounts(
            ListAccountsQuery(
                tags=["legacy"],
                page=1,
                page_size=10,
                sort_by="token",
                sort_desc=False,
            )
        )

        self.assertEqual(["tok-old"], [item.token for item in page.items])


if __name__ == "__main__":
    unittest.main()
