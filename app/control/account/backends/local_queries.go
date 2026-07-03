package backends

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	account "github.com/dslzl/gork/app/control/account"
)

func (r *LocalAccountRepository) RuntimeSnapshot(ctx context.Context) (account.RuntimeSnapshot, error) {
	r.lock.Lock()
	defer r.lock.Unlock()
	db, err := r.connect(ctx)
	if err != nil {
		return account.RuntimeSnapshot{}, err
	}
	defer db.Close()
	revision, err := getLocalRevision(ctx, db)
	if err != nil {
		return account.RuntimeSnapshot{}, err
	}
	items, err := queryLocalAccounts(ctx, db, "WHERE deleted_at IS NULL")
	if err != nil {
		return account.RuntimeSnapshot{}, err
	}
	return account.RuntimeSnapshot{Revision: revision, Items: items}, nil
}

func (r *LocalAccountRepository) ScanChanges(
	ctx context.Context,
	sinceRevision int,
	limit int,
) (account.AccountChangeSet, error) {
	r.lock.Lock()
	defer r.lock.Unlock()
	if limit <= 0 {
		limit = account.AccountScanChangesDefaultLimit
	}
	db, err := r.connect(ctx)
	if err != nil {
		return account.AccountChangeSet{}, err
	}
	defer db.Close()
	return scanLocalChanges(ctx, db, sinceRevision, limit)
}

func (r *LocalAccountRepository) GetAccounts(
	ctx context.Context,
	tokens []string,
) ([]account.AccountRecord, error) {
	if len(tokens) == 0 {
		return []account.AccountRecord{}, nil
	}
	r.lock.Lock()
	defer r.lock.Unlock()
	db, err := r.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	args := make([]any, len(tokens))
	for i, token := range tokens {
		args[i] = token
	}
	where := "WHERE token IN (" + placeholders(len(tokens)) + ")"
	return queryLocalAccounts(ctx, db, where, args...)
}

func (r *LocalAccountRepository) ListAccounts(
	ctx context.Context,
	query account.ListAccountsQuery,
) (account.AccountPage, error) {
	query = normalizeLocalListQuery(query)
	r.lock.Lock()
	defer r.lock.Unlock()
	db, err := r.connect(ctx)
	if err != nil {
		return account.AccountPage{}, err
	}
	defer db.Close()
	return listLocalAccounts(ctx, db, query)
}

func scanLocalChanges(
	ctx context.Context,
	db localSQLRunner,
	sinceRevision int,
	limit int,
) (account.AccountChangeSet, error) {
	revision, err := getLocalRevision(ctx, db)
	if err != nil {
		return account.AccountChangeSet{}, err
	}
	nextRevision, ok, err := nextLocalChangeRevision(ctx, db, sinceRevision)
	if err != nil {
		return account.AccountChangeSet{}, err
	}
	if !ok {
		changes := account.NewAccountChangeSet()
		changes.Revision = revision
		return changes, nil
	}
	rows, err := db.QueryContext(
		ctx,
		"SELECT "+localAccountColumns+" FROM "+localAccountTable+
			" WHERE revision = ? ORDER BY token",
		nextRevision,
	)
	if err != nil {
		return account.AccountChangeSet{}, err
	}
	defer rows.Close()
	return scanLocalChangeRows(rows, nextRevision, nextRevision < revision)
}

func nextLocalChangeRevision(ctx context.Context, db localSQLRunner, sinceRevision int) (int, bool, error) {
	var next sql.NullInt64
	if err := db.QueryRowContext(ctx, "SELECT MIN(revision) FROM "+localAccountTable+" WHERE revision > ?", sinceRevision).Scan(&next); err != nil {
		return 0, false, err
	}
	if !next.Valid {
		return 0, false, nil
	}
	return int(next.Int64), true, nil
}

func scanLocalChangeRows(
	rows *sql.Rows,
	revision int,
	hasMore bool,
) (account.AccountChangeSet, error) {
	changes := account.NewAccountChangeSet()
	changes.Revision = revision
	for rows.Next() {
		record, err := scanLocalAccount(rows)
		if err != nil {
			return account.AccountChangeSet{}, err
		}
		if record.IsDeleted() {
			changes.DeletedTokens = append(changes.DeletedTokens, record.Token)
		} else {
			changes.Items = append(changes.Items, record)
		}
	}
	if err := rows.Err(); err != nil {
		return account.AccountChangeSet{}, err
	}
	changes.HasMore = hasMore
	return changes, nil
}

func listLocalAccounts(
	ctx context.Context,
	db localSQLRunner,
	query account.ListAccountsQuery,
) (account.AccountPage, error) {
	whereSQL, params := localListWhere(query)
	total, err := countLocalAccounts(ctx, db, whereSQL, params)
	if err != nil {
		return account.AccountPage{}, err
	}
	orderSQL := "ORDER BY " + safeLocalSort(query.SortBy) + " " + localOrderDir(query.SortDesc)
	offset := (query.Page - 1) * query.PageSize
	rows, err := db.QueryContext(
		ctx,
		"SELECT "+localAccountColumns+" FROM "+localAccountTable+" "+
			whereSQL+" "+orderSQL+" LIMIT ? OFFSET ?",
		append(params, query.PageSize, offset)...,
	)
	if err != nil {
		return account.AccountPage{}, err
	}
	defer rows.Close()
	items, err := scanLocalAccountRows(rows)
	if err != nil {
		return account.AccountPage{}, err
	}
	revision, err := getLocalRevision(ctx, db)
	if err != nil {
		return account.AccountPage{}, err
	}
	return account.AccountPage{
		Items:      items,
		Total:      total,
		Page:       query.Page,
		PageSize:   query.PageSize,
		TotalPages: maxInt(1, (total+query.PageSize-1)/query.PageSize),
		Revision:   revision,
	}, nil
}

func queryLocalAccounts(
	ctx context.Context,
	db localSQLRunner,
	whereSQL string,
	args ...any,
) ([]account.AccountRecord, error) {
	rows, err := db.QueryContext(
		ctx,
		"SELECT "+localAccountColumns+" FROM "+localAccountTable+" "+whereSQL,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLocalAccountRows(rows)
}

func scanLocalAccountRows(rows *sql.Rows) ([]account.AccountRecord, error) {
	items := []account.AccountRecord{}
	for rows.Next() {
		record, err := scanLocalAccount(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	return items, rows.Err()
}

func localListWhere(query account.ListAccountsQuery) (string, []any) {
	parts := []string{}
	params := []any{}
	if !query.IncludeDeleted {
		parts = append(parts, "deleted_at IS NULL")
	}
	if query.Pool != nil && *query.Pool != "" {
		parts = append(parts, "pool = ?")
		params = append(params, *query.Pool)
	}
	if query.Status != nil {
		parts = append(parts, "status = ?")
		params = append(params, query.Status.String())
	}
	for _, tag := range query.Tags {
		parts = append(parts, "tags LIKE ?")
		params = append(params, "%\""+tag+"\"%")
	}
	for _, tag := range query.ExcludeTags {
		parts = append(parts, "tags NOT LIKE ?")
		params = append(params, "%\""+tag+"\"%")
	}
	if len(parts) == 0 {
		return "", params
	}
	return "WHERE " + strings.Join(parts, " AND "), params
}

func countLocalAccounts(ctx context.Context, db localSQLRunner, whereSQL string, params []any) (int, error) {
	var total int
	err := db.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM "+localAccountTable+" "+whereSQL,
		params...,
	).Scan(&total)
	return total, err
}

func normalizeLocalListQuery(query account.ListAccountsQuery) account.ListAccountsQuery {
	if isZeroLocalListQuery(query) {
		return account.DefaultListAccountsQuery()
	}
	query.Normalize()
	return query
}

func isZeroLocalListQuery(query account.ListAccountsQuery) bool {
	return query.Page == 0 && query.PageSize == 0 && query.Pool == nil &&
		query.Status == nil && query.Tags == nil && query.ExcludeTags == nil &&
		!query.IncludeDeleted && query.SortBy == "" && !query.SortDesc
}

func safeLocalSort(sortBy string) string {
	switch sortBy {
	case "updated_at", "created_at", "last_use_at", "token", "usage_use_count", "usage_fail_count":
		return sortBy
	default:
		return "updated_at"
	}
}

func localOrderDir(desc bool) string {
	if desc {
		return "DESC"
	}
	return "ASC"
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimRight(strings.Repeat("?,", n), ",")
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func localQueryError(name string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", name, err)
}
