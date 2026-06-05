"""Admin token CRUD — list, import, delete, replace pool.

Performance notes:
  - DI-injected repo (no try/except per call)
  - orjson direct output (bypasses stdlib json)
  - Quota dict: zero deserialization — reads r.quota directly
  - Import refresh: reuses app.state.refresh_service singleton
"""

import asyncio
import re
from typing import TYPE_CHECKING

import orjson
from fastapi import APIRouter, Body, Depends, File, Form, Query, Request, UploadFile
from fastapi.responses import Response
from pydantic import BaseModel, RootModel

from app.platform.errors import AppError, ErrorKind, ValidationError
from app.platform.logging.logger import logger
from app.platform.runtime.clock import now_ms
from app.platform.runtime.task import create_task as create_runtime_task, expire_task
from app.control.account.commands import (
    AccountPatch,
    AccountUpsert,
    BulkReplacePoolCommand,
    ListAccountsQuery,
)
from app.control.account.enums import AccountStatus

if TYPE_CHECKING:
    from app.control.account.refresh import AccountRefreshService
    from app.control.account.repository import AccountRepository

from . import get_refresh_svc, get_repo

router = APIRouter(tags=["Admin - Tokens"])

# ---------------------------------------------------------------------------
# Token sanitisation
# ---------------------------------------------------------------------------

_TOKEN_TRANS = str.maketrans({
    "\u2010": "-", "\u2011": "-", "\u2012": "-",
    "\u2013": "-", "\u2014": "-", "\u2212": "-",
    "\u00a0": " ", "\u2007": " ", "\u202f": " ",
    "\u200b": "", "\u200c": "", "\u200d": "", "\ufeff": "",
})
_STRIP_RE = re.compile(r"\s+")


def _sanitize(value: str) -> str:
    tok = str(value or "").translate(_TOKEN_TRANS)
    tok = _STRIP_RE.sub("", tok)
    if tok.startswith("sso="):
        tok = tok[4:]
    return tok.encode("ascii", errors="ignore").decode("ascii")


def _mask(token: str) -> str:
    return f"{token[:8]}...{token[-8:]}" if len(token) > 20 else token


# ---------------------------------------------------------------------------
# Request models
# ---------------------------------------------------------------------------

class ReplacePoolRequest(BaseModel):
    pool: str
    tokens: list[str]
    tags: list[str] = []


class AddTokensRequest(BaseModel):
    tokens: list[str]
    pool: str = "basic"
    tags: list[str] = []


class EditTokenRequest(BaseModel):
    old_token: str
    token: str
    pool: str = "basic"


class ToggleTokenDisabledRequest(BaseModel):
    token: str
    disabled: bool


class ToggleTokensDisabledRequest(BaseModel):
    tokens: list[str]
    disabled: bool


class TokenImportItem(BaseModel):
    token: str
    tags: list[str] = []


class SaveTokensRequest(RootModel[dict[str, list[str | TokenImportItem]]]):
    """Bulk-save payload keyed by pool name."""


_IMPORT_CHUNK_SIZE = 500


# ---------------------------------------------------------------------------
# Serialisation — zero-copy quota extraction
# ---------------------------------------------------------------------------

def _quota_brief(q: dict) -> dict:
    """Extract {auto, fast, expert, heavy, console} with only remaining/total from stored quota dict."""
    out = {}
    for mode in ("auto", "fast", "expert", "heavy", "console"):
        v = q.get(mode)
        if isinstance(v, dict):
            out[mode] = {
                "remaining": int(v.get("remaining", 0) or 0),
                "total": int(v.get("total", 0) or 0),
            }
    return out


def _serialize_record(r) -> dict:
    return {
        "token":       r.token,
        "pool":        r.pool or "basic",
        "status":      r.status,
        "quota":       _quota_brief(r.quota) if isinstance(r.quota, dict) else {},
        "use_count":   r.usage_use_count or 0,
        "last_used_at": r.last_use_at,
        "tags":        r.tags or [],
    }


def _json(data) -> Response:
    """orjson fast-path response."""
    return Response(content=orjson.dumps(data), media_type="application/json")


def _dedupe_sanitized(values: list[str]) -> list[str]:
    cleaned: list[str] = []
    seen: set[str] = set()
    for value in values:
        token = _sanitize(value)
        if token and token not in seen:
            seen.add(token)
            cleaned.append(token)
    return cleaned


def _normalise_tags(raw: str | list[str] | None) -> list[str]:
    if raw is None:
        return []
    if isinstance(raw, str):
        parts = raw.split(",")
    else:
        parts = raw
    tags: list[str] = []
    for item in parts:
        tag = str(item or "").strip()
        if tag and tag not in tags:
            tags.append(tag)
    return tags


def _param_value(value, default=None):
    if hasattr(value, "default") and value.__class__.__module__.startswith("fastapi."):
        return default if value.default is ... else value.default
    return value


def _status_filter(value: str | AccountStatus | None) -> AccountStatus | None:
    if value is None:
        return None
    text = str(value).strip().lower()
    if not text or text == "all":
        return None
    if text == "invalid":
        return AccountStatus.EXPIRED
    try:
        return AccountStatus(text)
    except ValueError as exc:
        raise ValidationError("Invalid status filter", param="status") from exc


def _pool_filter(value: str | None) -> str | None:
    text = str(value or "").strip().lower()
    return None if not text or text == "all" else text


def _tags_for_nsfw(value: str | None) -> tuple[list[str], list[str]]:
    mode = str(value or "").strip().lower()
    if not mode or mode == "all":
        return [], []
    if mode == "enabled":
        return ["nsfw"], []
    if mode == "disabled":
        return [], ["nsfw"]
    raise ValidationError("Invalid NSFW filter", param="nsfw")


async def _collect_all(repo: "AccountRepository", query: ListAccountsQuery | None = None) -> list:
    items: list = []
    page_num = 1
    base = query or ListAccountsQuery(page_size=2000)
    while True:
        page = await repo.list_accounts(base.model_copy(update={"page": page_num, "page_size": 2000}))
        items.extend(page.items)
        if page_num * 2000 >= page.total:
            break
        page_num += 1
    return items


def _is_invalid_status(status: AccountStatus | str) -> bool:
    return str(status) not in {
        AccountStatus.ACTIVE.value,
        AccountStatus.COOLING.value,
        AccountStatus.DISABLED.value,
    }


def _build_facets(records: list) -> dict:
    status = {"all": 0, "active": 0, "cooling": 0, "invalid": 0, "disabled": 0}
    nsfw = {"all": 0, "enabled": 0, "disabled": 0}
    pools: dict[str, int] = {"all": 0, "basic": 0, "super": 0, "heavy": 0}
    stats = {
        "active": 0,
        "cooling": 0,
        "invalid": 0,
        "disabled": 0,
        "calls": 0,
        "success": 0,
        "fail": 0,
        "qa": 0,
        "qf": 0,
        "qe": 0,
        "qh": 0,
        "qc": 0,
    }
    for record in records:
        pool = record.pool or "basic"
        pools["all"] += 1
        pools[pool] = pools.get(pool, 0) + 1

        state = str(record.status)
        status["all"] += 1
        if state == AccountStatus.ACTIVE.value:
            status["active"] += 1
            stats["active"] += 1
        elif state == AccountStatus.COOLING.value:
            status["cooling"] += 1
            stats["cooling"] += 1
        elif state == AccountStatus.DISABLED.value:
            status["disabled"] += 1
            stats["disabled"] += 1
        else:
            status["invalid"] += 1
            stats["invalid"] += 1

        enabled = "nsfw" in (record.tags or [])
        nsfw["all"] += 1
        nsfw["enabled" if enabled else "disabled"] += 1

        success = record.usage_use_count or 0
        fail = record.usage_fail_count or 0
        stats["success"] += success
        stats["fail"] += fail
        stats["calls"] += success + fail
        quota = record.quota or {}
        stats["qa"] += int((quota.get("auto") or {}).get("remaining", 0) or 0)
        stats["qf"] += int((quota.get("fast") or {}).get("remaining", 0) or 0)
        stats["qe"] += int((quota.get("expert") or {}).get("remaining", 0) or 0)
        stats["qh"] += int((quota.get("heavy") or {}).get("remaining", 0) or 0)
        stats["qc"] += int((quota.get("console") or {}).get("remaining", 0) or 0)

    return {"status": status, "nsfw": nsfw, "pools": pools, "stats": stats}


def _tokens_from_text(raw: str) -> list[str]:
    return _dedupe_sanitized(re.split(r"[\r\n,]+", raw or ""))


def _pool_payload_from_json(raw: str) -> dict[str, list[AccountUpsert]]:
    try:
        parsed = orjson.loads(raw)
    except orjson.JSONDecodeError as exc:
        raise ValidationError("Invalid JSON import file", param="file") from exc
    if not isinstance(parsed, dict):
        raise ValidationError("JSON import file must be {pool:[tokens]}", param="file")

    payload: dict[str, list[AccountUpsert]] = {}
    for pool_name, items in parsed.items():
        if not isinstance(items, list):
            continue
        upserts: list[AccountUpsert] = []
        seen: set[str] = set()
        for item in items:
            item_data = {"token": item} if isinstance(item, str) else item
            if not isinstance(item_data, dict):
                continue
            token = _sanitize(item_data.get("token", ""))
            if not token or token in seen:
                continue
            seen.add(token)
            upserts.append(AccountUpsert(
                token=token,
                pool=str(pool_name or "basic").strip().lower(),
                tags=_normalise_tags(item_data.get("tags")),
            ))
        if upserts:
            payload[str(pool_name or "basic").strip().lower()] = upserts
    if not payload:
        raise ValidationError("No valid tokens provided", param="file")
    return payload


async def _run_add_import(
    *,
    repo: "AccountRepository",
    refresh_svc: "AccountRefreshService",
    task,
    pool: str,
    tokens: list[str],
    tags: list[str],
) -> None:
    saved = 0
    skipped = 0
    refresh_tokens: list[str] = []
    try:
        for start in range(0, len(tokens), _IMPORT_CHUNK_SIZE):
            if task.cancelled:
                task.finish_cancelled()
                return
            chunk = tokens[start:start + _IMPORT_CHUNK_SIZE]
            existing = {r.token for r in await repo.get_accounts(chunk) if not r.is_deleted()}
            new_tokens = [token for token in chunk if token not in existing]
            skipped += len(chunk) - len(new_tokens)
            if new_tokens:
                upserts = [AccountUpsert(token=token, pool=pool, tags=tags) for token in new_tokens]
                result = await repo.upsert_accounts(upserts)
                saved += result.upserted or len(new_tokens)
                refresh_tokens.extend(new_tokens)
            task.record(True, count=len(chunk), detail={"saved": saved, "skipped": skipped})

        if refresh_tokens:
            await refresh_svc.refresh_on_import(refresh_tokens)
        task.finish({
            "status": "success",
            "summary": {
                "total": len(tokens),
                "ok": saved,
                "fail": 0,
                "skipped": skipped,
            },
        })
    except Exception as exc:
        task.fail_task(str(exc))
    finally:
        asyncio.create_task(expire_task(task.id, 300))


async def _run_replace_import(
    *,
    repo: "AccountRepository",
    refresh_svc: "AccountRefreshService",
    task,
    payload: dict[str, list[AccountUpsert]],
) -> None:
    saved = 0
    refresh_tokens: list[str] = []
    try:
        for pool_name, upserts in payload.items():
            if task.cancelled:
                task.finish_cancelled()
                return
            result = await repo.replace_pool(BulkReplacePoolCommand(pool=pool_name, upserts=upserts))
            saved += result.upserted or len(upserts)
            refresh_tokens.extend(item.token for item in upserts)
            task.record(True, count=len(upserts), detail={"pool": pool_name, "saved": saved})

        if refresh_tokens:
            await refresh_svc.refresh_on_import(refresh_tokens)
        task.finish({
            "status": "success",
            "summary": {
                "total": sum(len(items) for items in payload.values()),
                "ok": saved,
                "fail": 0,
                "skipped": 0,
            },
        })
    except Exception as exc:
        task.fail_task(str(exc))
    finally:
        asyncio.create_task(expire_task(task.id, 300))


# ---------------------------------------------------------------------------
# Endpoints
# ---------------------------------------------------------------------------

@router.get("/tokens")
async def list_tokens(
    page: int = Query(1, ge=1),
    page_size: int = Query(50, ge=1, le=2000),
    pool: str | None = Query(None),
    status: str | None = Query(None),
    nsfw: str | None = Query(None),
    sort_by: str = Query("updated_at"),
    sort_desc: bool = Query(True),
    repo: "AccountRepository" = Depends(get_repo),
):
    """Return one token page plus lightweight global facets."""
    page = int(_param_value(page, 1))
    page_size = int(_param_value(page_size, 50))
    pool = _param_value(pool)
    status = _param_value(status)
    nsfw = _param_value(nsfw)
    sort_by = str(_param_value(sort_by, "updated_at"))
    sort_desc = bool(_param_value(sort_desc, True))
    include_tags, exclude_tags = _tags_for_nsfw(nsfw)
    query = ListAccountsQuery(
        page=page,
        page_size=page_size,
        pool=_pool_filter(pool),
        status=_status_filter(status),
        tags=include_tags,
        exclude_tags=exclude_tags,
        sort_by=sort_by,
        sort_desc=sort_desc,
    )
    result = await repo.list_accounts(query)
    facets = _build_facets(await _collect_all(repo))
    return _json({
        "tokens": [_serialize_record(r) for r in result.items],
        "pagination": {
            "total": result.total,
            "page": result.page,
            "page_size": result.page_size,
            "total_pages": result.total_pages,
        },
        "facets": facets,
        "revision": result.revision,
    })


@router.post("/tokens/import-async")
async def import_tokens_async(
    request: Request = None,
    pool: str = Form("basic"),
    mode: str = Form("add"),
    tokens_text: str | None = Form(None),
    tags: str | None = Form(None),
    file: UploadFile | None = File(None),
    repo: "AccountRepository" = Depends(get_repo),
    refresh_svc: "AccountRefreshService" = Depends(get_refresh_svc),
):
    """Start a background token import task."""
    pool = _param_value(pool, "basic")
    mode = _param_value(mode, "add")
    tokens_text = _param_value(tokens_text)
    tags = _param_value(tags)
    file = _param_value(file)
    requested_pool = (pool or "basic").strip().lower()
    requested_mode = (mode or "add").strip().lower()
    requested_tags = _normalise_tags(tags)

    if request is not None and file is None and not tokens_text:
        content_type = request.headers.get("content-type", "").lower()
        if "application/json" in content_type:
            try:
                data = await request.json()
            except Exception as exc:
                raise ValidationError("Invalid JSON body", param="body") from exc
            if isinstance(data, dict):
                requested_pool = str(data.get("pool") or requested_pool).strip().lower()
                requested_mode = str(data.get("mode") or requested_mode).strip().lower()
                requested_tags = _normalise_tags(data.get("tags") if "tags" in data else requested_tags)
                if "tokens_text" in data:
                    tokens_text = str(data.get("tokens_text") or "")
                elif isinstance(data.get("tokens"), list):
                    tokens_text = "\n".join(str(item) for item in data["tokens"])

    if file is not None:
        raw = (await file.read()).decode("utf-8-sig", errors="ignore")
        if (file.filename or "").lower().endswith(".json"):
            payload = _pool_payload_from_json(raw)
            total = sum(len(items) for items in payload.values())
            task = create_runtime_task(total)
            asyncio.create_task(_run_replace_import(
                repo=repo,
                refresh_svc=refresh_svc,
                task=task,
                payload=payload,
            ))
            return _json({"status": "success", "task_id": task.id, "total": total})
        tokens_text = raw

    if requested_mode == "replace" and tokens_text and tokens_text.lstrip().startswith("{"):
        payload = _pool_payload_from_json(tokens_text)
        total = sum(len(items) for items in payload.values())
        task = create_runtime_task(total)
        asyncio.create_task(_run_replace_import(
            repo=repo,
            refresh_svc=refresh_svc,
            task=task,
            payload=payload,
        ))
        return _json({"status": "success", "task_id": task.id, "total": total})

    tokens = _tokens_from_text(tokens_text or "")
    if not tokens:
        raise ValidationError("No valid tokens provided", param="tokens")
    task = create_runtime_task(len(tokens))
    asyncio.create_task(_run_add_import(
        repo=repo,
        refresh_svc=refresh_svc,
        task=task,
        pool=requested_pool,
        tokens=tokens,
        tags=requested_tags,
    ))
    return _json({"status": "success", "task_id": task.id, "total": len(tokens)})


@router.post("/tokens")
async def save_tokens(
    req: SaveTokensRequest,
    repo: "AccountRepository" = Depends(get_repo),
    refresh_svc: "AccountRefreshService" = Depends(get_refresh_svc),
):
    """Full pool replace — accepts {pool_name: [token_objects]} dict."""
    total_upserted = 0
    all_tokens: list[str] = []

    for pool_name, items in req.root.items():
        upserts = []
        for item in items:
            td = {"token": item} if isinstance(item, str) else item.model_dump()
            token_val = _sanitize(td.get("token", ""))
            if not token_val:
                continue
            upserts.append(AccountUpsert(token=token_val, pool=pool_name, tags=td.get("tags") or []))
        if upserts:
            await repo.replace_pool(BulkReplacePoolCommand(pool=pool_name, upserts=upserts))
            all_tokens.extend(u.token for u in upserts)
            total_upserted += len(upserts)

    logger.info("admin tokens saved across pools: saved_count={}", total_upserted)
    if all_tokens:
        asyncio.create_task(_refresh_imported(refresh_svc, all_tokens))
    return _json({"status": "success", "count": total_upserted})


@router.post("/tokens/add")
async def add_tokens(
    req: AddTokensRequest,
    repo: "AccountRepository" = Depends(get_repo),
    refresh_svc: "AccountRefreshService" = Depends(get_refresh_svc),
):
    requested_pool = (req.pool or "basic").strip().lower()
    sync_auto_detect = requested_pool == "auto"

    # Deduplicate and sanitize input
    cleaned: list[str] = []
    seen: set[str] = set()
    for token in req.tokens:
        tok = _sanitize(token)
        if tok and tok not in seen:
            seen.add(tok)
            cleaned.append(tok)
    if not cleaned:
        raise ValidationError("No valid tokens provided", param="tokens")

    # Only upsert tokens that are not already active — avoids overwriting quota/status.
    # Soft-deleted tokens are treated as non-existing so they can be restored.
    existing = {r.token for r in await repo.get_accounts(cleaned) if not r.is_deleted()}
    new_tokens = [t for t in cleaned if t not in existing]

    if not new_tokens:
        return _json({"status": "success", "count": 0, "skipped": len(cleaned)})

    upserts = [AccountUpsert(token=t, pool=requested_pool, tags=req.tags) for t in new_tokens]
    result = await repo.upsert_accounts(upserts)
    logger.info(
        "admin tokens added: pool={} added_count={} skipped_count={}",
        requested_pool,
        len(new_tokens),
        len(existing),
    )

    if sync_auto_detect:
        try:
            refresh_result = await refresh_svc.refresh_on_import(new_tokens)
            logger.info(
                "admin auto-detect quota sync completed: token_count={} refreshed={} failed={}",
                len(new_tokens), refresh_result.refreshed, refresh_result.failed,
            )
        except Exception as exc:
            logger.warning("admin auto-detect quota sync failed: token_count={} error={}", len(new_tokens), exc)
    else:
        asyncio.create_task(_refresh_imported(refresh_svc, new_tokens))

    return _json({
        "status": "success",
        "count": result.upserted or len(new_tokens),
        "skipped": len(existing),
        "synced": sync_auto_detect,
    })


@router.delete("/tokens")
async def delete_tokens(
    tokens: list[str] = Body(...),
    repo: "AccountRepository" = Depends(get_repo),
):
    cleaned = [t for t in (_sanitize(t) for t in tokens) if t]
    if not cleaned:
        raise ValidationError("No valid tokens provided", param="tokens")
    await repo.delete_accounts(cleaned)
    logger.info("admin tokens deleted: deleted_count={}", len(cleaned))
    return _json({"deleted": len(cleaned)})


@router.put("/tokens/edit")
async def edit_token(
    req: EditTokenRequest,
    repo: "AccountRepository" = Depends(get_repo),
):
    old_token = _sanitize(req.old_token)
    new_token = _sanitize(req.token)
    pool = (req.pool or "basic").strip().lower()

    if not old_token or not new_token:
        raise ValidationError("Token is required", param="token")

    records = await repo.get_accounts([old_token])
    if not records:
        raise AppError(
            "Account not found",
            kind=ErrorKind.VALIDATION,
            code="account_not_found",
            status=404,
        )
    record = records[0]

    if old_token != new_token:
        existing = await repo.get_accounts([new_token])
        if existing:
            raise AppError(
                "Target token already exists",
                kind=ErrorKind.VALIDATION,
                code="token_conflict",
                status=409,
            )

    await repo.upsert_accounts([AccountUpsert(
        token=new_token,
        pool=pool,
        tags=record.tags,
        ext=record.ext,
    )])

    if old_token == new_token:
        logger.info("admin token updated: token={} pool={}", _mask(new_token), pool)
        return _json({"status": "success", "token": new_token, "pool": pool})

    qs = record.quota_set()
    await repo.patch_accounts([AccountPatch(
        token=new_token,
        status=record.status,
        tags=record.tags,
        quota_auto=qs.auto.to_dict(),
        quota_fast=qs.fast.to_dict(),
        quota_expert=qs.expert.to_dict(),
        usage_use_delta=record.usage_use_count,
        usage_fail_delta=record.usage_fail_count,
        usage_sync_delta=record.usage_sync_count,
        last_use_at=record.last_use_at,
        last_fail_at=record.last_fail_at,
        last_fail_reason=record.last_fail_reason,
        last_sync_at=record.last_sync_at,
        last_clear_at=record.last_clear_at,
        state_reason=record.state_reason,
        ext_merge=record.ext,
    )])
    await repo.delete_accounts([old_token])

    logger.info("admin token replaced: previous_token={} current_token={} pool={}", _mask(old_token), _mask(new_token), pool)
    return _json({"status": "success", "token": new_token, "pool": pool})


@router.post("/tokens/disabled")
async def toggle_token_disabled(
    req: ToggleTokenDisabledRequest,
    repo: "AccountRepository" = Depends(get_repo),
):
    token = _sanitize(req.token)
    if not token:
        raise ValidationError("Token is required", param="token")

    records = await repo.get_accounts([token])
    if not records:
        raise AppError(
            "Account not found",
            kind=ErrorKind.VALIDATION,
            code="account_not_found",
            status=404,
        )
    record = records[0]

    if req.disabled:
        await repo.patch_accounts([AccountPatch(
            token=token,
            status=AccountStatus.DISABLED,
            state_reason="operator_disabled",
            ext_merge={
                **record.ext,
                "disabled_at": now_ms(),
                "disabled_reason": "operator_disabled",
            },
        )])
        logger.info("admin token disabled: token={}", _mask(token))
        return _json({"status": "success", "token": token, "disabled": True})

    await repo.patch_accounts([AccountPatch(
        token=token,
        status=AccountStatus.ACTIVE,
        clear_failures=True,
    )])
    logger.info("admin token restored: token={}", _mask(token))
    return _json({"status": "success", "token": token, "disabled": False})


@router.post("/tokens/disabled/batch")
async def toggle_tokens_disabled(
    req: ToggleTokensDisabledRequest,
    repo: "AccountRepository" = Depends(get_repo),
):
    cleaned: list[str] = []
    seen: set[str] = set()
    for raw in req.tokens:
        token = _sanitize(raw)
        if token and token not in seen:
            seen.add(token)
            cleaned.append(token)
    if not cleaned:
        raise ValidationError("No valid tokens provided", param="tokens")

    records = await repo.get_accounts(cleaned)
    if not records:
        raise AppError(
            "No matching accounts found",
            kind=ErrorKind.VALIDATION,
            code="account_not_found",
            status=404,
        )

    ts = now_ms()
    patches: list[AccountPatch] = []
    for record in records:
        if req.disabled:
            patches.append(AccountPatch(
                token=record.token,
                status=AccountStatus.DISABLED,
                state_reason="operator_disabled",
                ext_merge={
                    **record.ext,
                    "disabled_at": ts,
                    "disabled_reason": "operator_disabled",
                },
            ))
        else:
            patches.append(AccountPatch(
                token=record.token,
                status=AccountStatus.ACTIVE,
                clear_failures=True,
            ))

    result = await repo.patch_accounts(patches)
    logger.info(
        "admin tokens disabled batch updated: disabled={} requested_count={} patched_count={}",
        req.disabled,
        len(cleaned),
        result.patched,
    )
    return _json({
        "status": "success",
        "disabled": req.disabled,
        "summary": {
            "total": len(cleaned),
            "ok": result.patched,
            "fail": max(0, len(cleaned) - result.patched),
        },
    })


@router.put("/tokens/pool")
async def replace_pool(
    req: ReplacePoolRequest,
    repo: "AccountRepository" = Depends(get_repo),
    refresh_svc: "AccountRefreshService" = Depends(get_refresh_svc),
):
    cleaned = [t for t in (_sanitize(t) for t in req.tokens) if t]
    upserts = [AccountUpsert(token=t, pool=req.pool, tags=req.tags) for t in cleaned]
    await repo.replace_pool(BulkReplacePoolCommand(pool=req.pool, upserts=upserts))
    logger.info("admin pool replaced: pool={} token_count={}", req.pool, len(cleaned))
    if cleaned:
        asyncio.create_task(_refresh_imported(refresh_svc, cleaned))
    return _json({"pool": req.pool, "count": len(cleaned)})


# ---------------------------------------------------------------------------
# Fire-and-forget import refresh
# ---------------------------------------------------------------------------

async def _refresh_imported(svc: "AccountRefreshService", tokens: list[str]) -> None:
    try:
        await svc.refresh_on_import(tokens)
        logger.info("admin import quota sync completed: token_count={}", len(tokens))
    except Exception as exc:
        logger.warning("admin import quota sync failed: token_count={} error={}", len(tokens), exc)
