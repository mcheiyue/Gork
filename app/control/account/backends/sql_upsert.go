package backends

import (
	"context"
	"strings"

	account "github.com/jiujiu532/grok2api/app/control/account"
	platformruntime "github.com/jiujiu532/grok2api/app/platform/runtime"
)

func (r *SQLAccountRepository) upsertAccountsInTx(
	ctx context.Context,
	items []account.AccountUpsert,
) (account.AccountMutationResult, error) {
	tx, err := r.beginSQLMutation(ctx)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	defer tx.Rollback()
	revision, err := bumpSQLRevision(ctx, tx, r.dialect)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	count, err := upsertSQLAccounts(ctx, tx, r.dialect, items, revision)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return account.AccountMutationResult{}, err
	}
	return account.AccountMutationResult{Upserted: count, Revision: revision}, nil
}

func upsertSQLAccounts(
	ctx context.Context,
	tx localSQLRunner,
	dialect SQLDialect,
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
		if err := upsertSQLAccount(ctx, tx, dialect, item, token, pool, ts, revision); err != nil {
			return 0, err
		}
		count++
	}
	return count, nil
}

func upsertSQLAccount(
	ctx context.Context,
	tx localSQLRunner,
	dialect SQLDialect,
	item account.AccountUpsert,
	token string,
	pool string,
	ts int64,
	revision int,
) error {
	item.Normalize()
	quota := account.DefaultQuotaSet(pool)
	tags, err := jsonString(item.Tags)
	if err != nil {
		return err
	}
	ext, err := jsonString(item.Ext)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, sqlUpsertStatement(dialect), token, pool, account.AccountStatusActive.String(),
		ts, ts, nil, tags, mustQuotaJSON(quota.Auto), mustQuotaJSON(quota.Fast),
		mustQuotaJSON(quota.Expert), optionalQuotaJSON(quota.Heavy),
		optionalQuotaJSON(quota.Grok43), optionalQuotaJSON(quota.Console),
		0, 0, 0, ext, revision)
	return err
}

func sqlUpsertStatement(dialect SQLDialect) string {
	columns := []string{
		"token", "pool", "status", "created_at", "updated_at", "deleted_at",
		"tags", "quota_auto", "quota_fast", "quota_expert", "quota_heavy",
		"quota_grok_4_3", "quota_console", "usage_use_count",
		"usage_fail_count", "usage_sync_count", "ext", "revision",
	}
	updateColumns := []string{
		"pool", "status", "updated_at", "deleted_at", "tags", "quota_auto",
		"quota_fast", "quota_expert", "quota_heavy", "quota_grok_4_3",
		"quota_console", "usage_use_count", "usage_fail_count",
		"usage_sync_count", "ext", "revision",
	}
	head := "INSERT INTO accounts (" + strings.Join(columns, ", ") + ") VALUES (" +
		sqlPlaceholders(dialect, 1, len(columns)) + ")"
	if dialect == SQLDialectMySQL {
		return head + " ON DUPLICATE KEY UPDATE " + mysqlUpsertAssignments(updateColumns)
	}
	return head + " ON CONFLICT(token) DO UPDATE SET " + conflictUpsertAssignments(updateColumns)
}

func conflictUpsertAssignments(columns []string) string {
	sets := make([]string, len(columns))
	for i, column := range columns {
		sets[i] = column + " = excluded." + column
	}
	return strings.Join(sets, ", ")
}

func mysqlUpsertAssignments(columns []string) string {
	sets := make([]string, len(columns))
	for i, column := range columns {
		sets[i] = column + " = VALUES(" + column + ")"
	}
	return strings.Join(sets, ", ")
}
