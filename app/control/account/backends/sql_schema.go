package backends

import (
	"context"
	"database/sql"
	"strconv"
)

func ensureSQLSchema(ctx context.Context, db localSQLRunner, dialect SQLDialect) error {
	if dialect == SQLDialectSQLite {
		return ensureLocalSchema(ctx, db)
	}
	for _, stmt := range sqlSchemaStatements(dialect) {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if err := seedSQLRevision(ctx, db, dialect); err != nil {
		return err
	}
	if err := ensureSQLColumns(ctx, db, dialect); err != nil {
		return err
	}
	return ensureSQLIndexes(ctx, db, dialect)
}

func getSQLRevision(ctx context.Context, db localSQLRunner, dialect SQLDialect) (int, error) {
	var raw string
	query := "SELECT value FROM account_meta WHERE " + sqlMetaKeyColumn(dialect) + " = " + sqlBind(dialect, 1)
	err := db.QueryRowContext(ctx, query, "revision").Scan(&raw)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	revision, err := strconv.Atoi(raw)
	return revision, err
}

func bumpSQLRevision(ctx context.Context, db localSQLRunner, dialect SQLDialect) (int, error) {
	_, err := db.ExecContext(ctx, sqlBumpRevisionSQL(dialect), "revision")
	if err != nil {
		return 0, err
	}
	return getSQLRevision(ctx, db, dialect)
}

func seedSQLRevision(ctx context.Context, db localSQLRunner, dialect SQLDialect) error {
	_, err := db.ExecContext(ctx, sqlSeedRevisionSQL(dialect))
	return err
}

func ensureSQLColumns(ctx context.Context, db localSQLRunner, dialect SQLDialect) error {
	existing, err := sqlTableColumns(ctx, db, dialect, "accounts")
	if err != nil {
		return err
	}
	for _, column := range []string{"quota_grok_4_3", "quota_console"} {
		if existing[column] {
			continue
		}
		if err := addSQLJSONColumn(ctx, db, dialect, column); err != nil {
			return err
		}
	}
	return nil
}

func sqlTableColumns(
	ctx context.Context,
	db localSQLRunner,
	dialect SQLDialect,
	table string,
) (map[string]bool, error) {
	query := "SELECT column_name FROM information_schema.columns WHERE table_name = " + sqlBind(dialect, 1)
	args := []any{table}
	if dialect == SQLDialectMySQL {
		query = "SELECT COLUMN_NAME FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ?"
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}

func addSQLJSONColumn(ctx context.Context, db localSQLRunner, dialect SQLDialect, name string) error {
	if dialect == SQLDialectMySQL {
		if _, err := db.ExecContext(ctx, "ALTER TABLE accounts ADD COLUMN "+name+" TEXT"); err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, "UPDATE accounts SET "+name+" = '{}' WHERE "+name+" IS NULL"); err != nil {
			return err
		}
		_, err := db.ExecContext(ctx, "ALTER TABLE accounts MODIFY COLUMN "+name+" TEXT NOT NULL")
		return err
	}
	_, err := db.ExecContext(ctx, "ALTER TABLE accounts ADD COLUMN "+name+" TEXT NOT NULL DEFAULT '{}'")
	return err
}

func ensureSQLIndexes(ctx context.Context, db localSQLRunner, dialect SQLDialect) error {
	if dialect != SQLDialectMySQL {
		return nil
	}
	var count int
	err := db.QueryRowContext(ctx, mysqlIndexExistsSQL, "accounts", "idx_acc_revision").Scan(&count)
	if err != nil || count > 0 {
		return err
	}
	_, err = db.ExecContext(ctx, "CREATE INDEX idx_acc_revision ON accounts (revision)")
	return err
}

func sqlMetaKeyColumn(dialect SQLDialect) string {
	if dialect == SQLDialectMySQL {
		return "`key`"
	}
	return "key"
}

func sqlBind(dialect SQLDialect, n int) string {
	if dialect == SQLDialectPostgreSQL {
		return "$" + strconv.Itoa(n)
	}
	return "?"
}
