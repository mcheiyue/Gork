package backends

import (
	"context"
	"strings"

	account "github.com/jiujiu532/grok2api/app/control/account"
)

func (r *SQLAccountRepository) RuntimeSnapshot(ctx context.Context) (account.RuntimeSnapshot, error) {
	if err := r.ensureInitialized(ctx); err != nil {
		return account.RuntimeSnapshot{}, err
	}
	revision, err := getSQLRevision(ctx, r.db, r.dialect)
	if err != nil {
		return account.RuntimeSnapshot{}, err
	}
	items, err := querySQLAccounts(ctx, r.db, "WHERE deleted_at IS NULL")
	return account.RuntimeSnapshot{Revision: revision, Items: items}, err
}

func (r *SQLAccountRepository) ScanChanges(
	ctx context.Context,
	sinceRevision int,
	limit int,
) (account.AccountChangeSet, error) {
	if limit <= 0 {
		limit = account.AccountScanChangesDefaultLimit
	}
	if err := r.ensureInitialized(ctx); err != nil {
		return account.AccountChangeSet{}, err
	}
	return scanSQLChanges(ctx, r.db, r.dialect, sinceRevision, limit)
}

func (r *SQLAccountRepository) GetAccounts(
	ctx context.Context,
	tokens []string,
) ([]account.AccountRecord, error) {
	if len(tokens) == 0 {
		return []account.AccountRecord{}, nil
	}
	if err := r.ensureInitialized(ctx); err != nil {
		return nil, err
	}
	args := make([]any, len(tokens))
	for i, token := range tokens {
		args[i] = token
	}
	where := "WHERE token IN (" + sqlPlaceholders(r.dialect, 1, len(tokens)) + ")"
	return querySQLAccounts(ctx, r.db, where, args...)
}

func (r *SQLAccountRepository) ListAccounts(
	ctx context.Context,
	query account.ListAccountsQuery,
) (account.AccountPage, error) {
	query = normalizeLocalListQuery(query)
	if err := r.ensureInitialized(ctx); err != nil {
		return account.AccountPage{}, err
	}
	return listSQLAccounts(ctx, r.db, r.dialect, query)
}

func scanSQLChanges(
	ctx context.Context,
	db localSQLRunner,
	dialect SQLDialect,
	sinceRevision int,
	limit int,
) (account.AccountChangeSet, error) {
	revision, err := getSQLRevision(ctx, db, dialect)
	if err != nil {
		return account.AccountChangeSet{}, err
	}
	rows, err := db.QueryContext(ctx, sqlScanChangesQuery(dialect), sinceRevision, limit)
	if err != nil {
		return account.AccountChangeSet{}, err
	}
	defer rows.Close()
	return scanLocalChangeRows(rows, revision, limit)
}

func listSQLAccounts(
	ctx context.Context,
	db localSQLRunner,
	dialect SQLDialect,
	query account.ListAccountsQuery,
) (account.AccountPage, error) {
	whereSQL, params := sqlListWhere(dialect, query)
	total, err := countSQLAccounts(ctx, db, whereSQL, params)
	if err != nil {
		return account.AccountPage{}, err
	}
	offset := (query.Page - 1) * query.PageSize
	limitBind := len(params) + 1
	orderSQL := "ORDER BY " + safeSQLSort(query.SortBy) + " " + localOrderDir(query.SortDesc)
	rows, err := db.QueryContext(ctx, "SELECT "+localAccountColumns+" FROM accounts "+
		whereSQL+" "+orderSQL+" LIMIT "+sqlBind(dialect, limitBind)+" OFFSET "+sqlBind(dialect, limitBind+1),
		append(params, query.PageSize, offset)...)
	if err != nil {
		return account.AccountPage{}, err
	}
	defer rows.Close()
	items, err := scanLocalAccountRows(rows)
	if err != nil {
		return account.AccountPage{}, err
	}
	revision, err := getSQLRevision(ctx, db, dialect)
	if err != nil {
		return account.AccountPage{}, err
	}
	return account.AccountPage{Items: items, Total: total, Page: query.Page,
		PageSize: query.PageSize, TotalPages: maxInt(1, (total+query.PageSize-1)/query.PageSize),
		Revision: revision}, nil
}

func querySQLAccounts(
	ctx context.Context,
	db localSQLRunner,
	whereSQL string,
	args ...any,
) ([]account.AccountRecord, error) {
	rows, err := db.QueryContext(ctx, "SELECT "+localAccountColumns+" FROM accounts "+whereSQL, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLocalAccountRows(rows)
}

func countSQLAccounts(ctx context.Context, db localSQLRunner, whereSQL string, params []any) (int, error) {
	var total int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM accounts "+whereSQL, params...).Scan(&total)
	return total, err
}

func sqlListWhere(dialect SQLDialect, query account.ListAccountsQuery) (string, []any) {
	parts := []string{}
	params := []any{}
	add := func(condition string, value any) {
		params = append(params, value)
		parts = append(parts, condition+" "+sqlBind(dialect, len(params)))
	}
	if !query.IncludeDeleted {
		parts = append(parts, "deleted_at IS NULL")
	}
	if query.Pool != nil && *query.Pool != "" {
		add("pool =", *query.Pool)
	}
	if query.Status != nil {
		add("status =", query.Status.String())
	}
	for _, tag := range query.Tags {
		add("tags LIKE", "%\""+tag+"\"%")
	}
	for _, tag := range query.ExcludeTags {
		add("tags NOT LIKE", "%\""+tag+"\"%")
	}
	if len(parts) == 0 {
		return "", params
	}
	return "WHERE " + strings.Join(parts, " AND "), params
}
