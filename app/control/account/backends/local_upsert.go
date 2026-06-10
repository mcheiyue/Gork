package backends

import (
	"context"
	"fmt"

	account "github.com/jiujiu532/grok2api/app/control/account"
	platformruntime "github.com/jiujiu532/grok2api/app/platform/runtime"
)

func upsertLocalAccounts(
	ctx context.Context,
	tx localSQLRunner,
	items []account.AccountUpsert,
	revision int,
) (int, error) {
	ts := platformruntime.NowMS()
	count := 0
	for _, item := range items {
		token, pool, ok := normalizeLocalUpsert(item)
		if !ok {
			continue
		}
		affected, err := upsertLocalAccount(ctx, tx, item, token, pool, ts, revision)
		if err != nil {
			return 0, err
		}
		count += affected
	}
	return count, nil
}

func normalizeLocalUpsert(item account.AccountUpsert) (string, string, bool) {
	item.Normalize()
	record, err := account.NewAccountRecord(account.AccountRecord{Token: item.Token, Pool: item.Pool})
	if err != nil {
		return "", "", false
	}
	pool := "basic"
	if item.Pool == "basic" || item.Pool == "super" || item.Pool == "heavy" {
		pool = item.Pool
	}
	return record.Token, pool, true
}

func upsertLocalAccount(
	ctx context.Context,
	tx localSQLRunner,
	item account.AccountUpsert,
	token string,
	pool string,
	ts int64,
	revision int,
) (int, error) {
	item.Normalize()
	quota := account.DefaultQuotaSet(pool)
	tags, err := jsonString(item.Tags)
	if err != nil {
		return 0, err
	}
	ext, err := jsonString(item.Ext)
	if err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, localUpsertSQL, token, pool, ts, ts, tags,
		mustQuotaJSON(quota.Auto), mustQuotaJSON(quota.Fast), mustQuotaJSON(quota.Expert),
		optionalQuotaJSON(quota.Heavy), optionalQuotaJSON(quota.Grok43),
		optionalQuotaJSON(quota.Console), ext, revision)
	if err != nil {
		return 0, err
	}
	return affectedRows(result)
}

func mustQuotaJSON(window account.QuotaWindow) string {
	raw, err := jsonString(window.ToDict())
	if err != nil {
		panic(fmt.Errorf("quota json: %w", err))
	}
	return raw
}

func optionalQuotaJSON(window *account.QuotaWindow) string {
	if window == nil {
		return "{}"
	}
	return mustQuotaJSON(*window)
}

const localUpsertSQL = `
INSERT INTO accounts (
	token, pool, status, created_at, updated_at,
	tags, quota_auto, quota_fast, quota_expert, quota_heavy, quota_grok_4_3, quota_console,
	usage_use_count, usage_fail_count, usage_sync_count,
	ext, revision
) VALUES (
	?, ?, 'active', ?, ?,
	?, ?, ?, ?, ?, ?, ?,
	0, 0, 0, ?, ?
)
ON CONFLICT(token) DO UPDATE SET
	pool           = excluded.pool,
	status         = 'active',
	deleted_at     = NULL,
	updated_at     = excluded.updated_at,
	tags           = excluded.tags,
	quota_console  = excluded.quota_console,
	ext            = excluded.ext,
	revision       = excluded.revision
`
