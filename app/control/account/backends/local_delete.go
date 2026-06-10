package backends

import (
	"context"
	"database/sql"

	platformruntime "github.com/jiujiu532/grok2api/app/platform/runtime"
)

func deleteLocalAccounts(
	ctx context.Context,
	tx localSQLRunner,
	tokens []string,
	revision int,
) (int, error) {
	ts := platformruntime.NowMS()
	count := 0
	for _, token := range tokens {
		result, err := tx.ExecContext(ctx, localDeleteSQL, ts, ts, revision, token)
		if err != nil {
			return 0, err
		}
		count, err = affectedRows(result)
		if err != nil {
			return 0, err
		}
	}
	return count, nil
}

func deleteLocalPool(ctx context.Context, tx localSQLRunner, pool string, revision int) (int, error) {
	ts := platformruntime.NowMS()
	result, err := tx.ExecContext(ctx, localDeletePoolSQL, ts, ts, revision, pool)
	if err != nil {
		return 0, err
	}
	return affectedRows(result)
}

func affectedRows(result sql.Result) (int, error) {
	count, err := result.RowsAffected()
	return int(count), err
}

const localDeleteSQL = `
UPDATE accounts SET deleted_at = ?, updated_at = ?, revision = ?
WHERE token = ? AND deleted_at IS NULL
`

const localDeletePoolSQL = `
UPDATE accounts SET deleted_at = ?, updated_at = ?, revision = ?
WHERE pool = ? AND deleted_at IS NULL
`
