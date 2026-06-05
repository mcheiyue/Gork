import asyncio
import importlib.util
import sys
import tempfile
import types
import unittest
from pathlib import Path
from types import SimpleNamespace

import orjson

class _FakeEncoding:
    def encode(self, text: str) -> list[int]:
        return list(text.encode())


sys.modules.setdefault(
    "tiktoken",
    types.SimpleNamespace(
        encoding_for_model=lambda _name: _FakeEncoding(),
        get_encoding=lambda _name: _FakeEncoding(),
    ),
)

from app.control.account.backends.local import LocalAccountRepository
from app.control.account.commands import AccountUpsert, ListAccountsQuery
from app.platform.runtime.task import get_task


_admin_pkg = types.ModuleType("app.products.web.admin")
_admin_pkg.__path__ = []
_admin_pkg.get_refresh_svc = lambda *_args, **_kwargs: None
_admin_pkg.get_repo = lambda *_args, **_kwargs: None
sys.modules.setdefault("app.products.web.admin", _admin_pkg)

_tokens_path = Path(__file__).resolve().parents[1] / "app/products/web/admin/tokens.py"
_tokens_spec = importlib.util.spec_from_file_location("app.products.web.admin.tokens", _tokens_path)
tokens_admin = importlib.util.module_from_spec(_tokens_spec)
sys.modules[_tokens_spec.name] = tokens_admin
_tokens_spec.loader.exec_module(tokens_admin)


class _RefreshService:
    def __init__(self) -> None:
        self.calls: list[list[str]] = []

    async def refresh_on_import(self, tokens: list[str]):
        self.calls.append(list(tokens))
        return SimpleNamespace(refreshed=len(tokens), failed=0)


class AdminTokenLargeSetTests(unittest.IsolatedAsyncioTestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.repo = LocalAccountRepository(Path(self._tmp.name) / "accounts.db")
        self.repo._init_sync()

    async def _seed(self) -> None:
        await self.repo.upsert_accounts([
            AccountUpsert(token="tok-001", pool="basic", tags=["nsfw"]),
            AccountUpsert(token="tok-002", pool="basic", tags=[]),
            AccountUpsert(token="tok-003", pool="super", tags=["nsfw"]),
            AccountUpsert(token="tok-004", pool="heavy", tags=[]),
            AccountUpsert(token="tok-005", pool="basic", tags=[]),
        ])

    async def test_repository_filters_by_tags_before_pagination(self):
        await self._seed()

        page = await self.repo.list_accounts(
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

    async def test_list_tokens_returns_one_page_with_global_facets(self):
        await self._seed()

        response = await tokens_admin.list_tokens(
            page=2,
            page_size=2,
            sort_by="token",
            sort_desc=False,
            repo=self.repo,
        )
        body = orjson.loads(response.body)

        self.assertEqual(["tok-003", "tok-004"], [item["token"] for item in body["tokens"]])
        self.assertEqual(
            {
                "total": 5,
                "page": 2,
                "page_size": 2,
                "total_pages": 3,
            },
            body["pagination"],
        )
        self.assertEqual(3, body["facets"]["pools"]["basic"])
        self.assertEqual(1, body["facets"]["pools"]["super"])
        self.assertEqual(1, body["facets"]["pools"]["heavy"])
        self.assertEqual(5, body["facets"]["status"]["active"])
        self.assertEqual(2, body["facets"]["nsfw"]["enabled"])
        self.assertEqual(3, body["facets"]["nsfw"]["disabled"])

    async def test_async_text_import_reports_task_progress(self):
        refresh = _RefreshService()

        response = await tokens_admin.import_tokens_async(
            pool="basic",
            tokens_text="tok-100\ntok-101\ntok-100\n",
            repo=self.repo,
            refresh_svc=refresh,
        )
        body = orjson.loads(response.body)

        self.assertEqual("success", body["status"])
        self.assertEqual(2, body["total"])
        task = get_task(body["task_id"])
        self.assertIsNotNone(task)

        for _ in range(100):
            if task.status != "running":
                break
            await asyncio.sleep(0.01)

        self.assertEqual("done", task.status)
        self.assertEqual({"total": 2, "ok": 2, "fail": 0, "skipped": 0}, task.result["summary"])
        page = await self.repo.list_accounts(ListAccountsQuery(page=1, page_size=10, sort_by="token", sort_desc=False))
        self.assertEqual(["tok-100", "tok-101"], [item.token for item in page.items])
        self.assertEqual([["tok-100", "tok-101"]], refresh.calls)


if __name__ == "__main__":
    unittest.main()
