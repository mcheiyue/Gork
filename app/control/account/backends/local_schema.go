package backends

import (
	"context"
	"database/sql"
)

const (
	localAccountTable = "accounts"
	localMetaTable    = "account_meta"
)

type localSQLRunner interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func ensureLocalSchema(ctx context.Context, db localSQLRunner) error {
	if _, err := db.ExecContext(ctx, localSchemaSQL); err != nil {
		return err
	}
	if err := ensureLocalColumn(ctx, db, "quota_grok_4_3", "TEXT NOT NULL DEFAULT '{}'"); err != nil {
		return err
	}
	return ensureLocalColumn(ctx, db, "quota_console", "TEXT NOT NULL DEFAULT '{}'")
}

func ensureLocalColumn(ctx context.Context, db localSQLRunner, name string, ddl string) error {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+localAccountTable+")")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var columnName, columnType string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &columnName, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if columnName == name {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, "ALTER TABLE "+localAccountTable+" ADD COLUMN "+name+" "+ddl)
	return err
}

func bumpLocalRevision(ctx context.Context, db localSQLRunner) (int, error) {
	_, err := db.ExecContext(
		ctx,
		"UPDATE "+localMetaTable+" SET value = CAST(value AS INTEGER) + 1 WHERE key = 'revision'",
	)
	if err != nil {
		return 0, err
	}
	return getLocalRevision(ctx, db)
}

func getLocalRevision(ctx context.Context, db localSQLRunner) (int, error) {
	var revision int
	err := db.QueryRowContext(
		ctx,
		"SELECT CAST(value AS INTEGER) FROM "+localMetaTable+" WHERE key = 'revision'",
	).Scan(&revision)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return revision, err
}

const localSchemaSQL = `
CREATE TABLE IF NOT EXISTS account_meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
INSERT OR IGNORE INTO account_meta VALUES ('revision', '0');

CREATE TABLE IF NOT EXISTS accounts (
	token              TEXT    NOT NULL PRIMARY KEY,
	pool               TEXT    NOT NULL DEFAULT 'basic',
	status             TEXT    NOT NULL DEFAULT 'active',
	created_at         INTEGER NOT NULL,
	updated_at         INTEGER NOT NULL,
	tags               TEXT    NOT NULL DEFAULT '[]',
	quota_auto         TEXT    NOT NULL DEFAULT '{}',
	quota_fast         TEXT    NOT NULL DEFAULT '{}',
	quota_expert       TEXT    NOT NULL DEFAULT '{}',
	quota_heavy        TEXT    NOT NULL DEFAULT '{}',
	quota_grok_4_3     TEXT    NOT NULL DEFAULT '{}',
	quota_console      TEXT    NOT NULL DEFAULT '{}',
	usage_use_count    INTEGER NOT NULL DEFAULT 0,
	usage_fail_count   INTEGER NOT NULL DEFAULT 0,
	usage_sync_count   INTEGER NOT NULL DEFAULT 0,
	last_use_at        INTEGER,
	last_fail_at       INTEGER,
	last_fail_reason   TEXT,
	last_sync_at       INTEGER,
	last_clear_at      INTEGER,
	state_reason       TEXT,
	deleted_at         INTEGER,
	ext                TEXT    NOT NULL DEFAULT '{}',
	revision           INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_acc_revision
	ON accounts (revision);
CREATE INDEX IF NOT EXISTS idx_acc_pool_status
	ON accounts (pool, status);
CREATE INDEX IF NOT EXISTS idx_acc_deleted
	ON accounts (deleted_at) WHERE deleted_at IS NOT NULL;
`
