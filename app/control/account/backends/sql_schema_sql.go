package backends

func sqlSchemaStatements(dialect SQLDialect) []string {
	if dialect == SQLDialectMySQL {
		return mysqlSchemaStatements()
	}
	return postgresSchemaStatements()
}

func mysqlSchemaStatements() []string {
	return []string{
		"CREATE TABLE IF NOT EXISTS account_meta (`key` VARCHAR(128) PRIMARY KEY, value TEXT NOT NULL)",
		`CREATE TABLE IF NOT EXISTS accounts (
			token VARCHAR(512) NOT NULL PRIMARY KEY,
			pool TEXT NOT NULL,
			status TEXT NOT NULL,
			created_at BIGINT NOT NULL,
			updated_at BIGINT NOT NULL,
			tags TEXT NOT NULL,
			quota_auto TEXT NOT NULL,
			quota_fast TEXT NOT NULL,
			quota_expert TEXT NOT NULL,
			quota_heavy TEXT NOT NULL,
			quota_grok_4_3 TEXT NOT NULL,
			quota_console TEXT NOT NULL,
			usage_use_count INTEGER NOT NULL DEFAULT 0,
			usage_fail_count INTEGER NOT NULL DEFAULT 0,
			usage_sync_count INTEGER NOT NULL DEFAULT 0,
			last_use_at BIGINT,
			last_fail_at BIGINT,
			last_fail_reason TEXT,
			last_sync_at BIGINT,
			last_clear_at BIGINT,
			state_reason TEXT,
			deleted_at BIGINT,
			ext TEXT NOT NULL,
			revision BIGINT NOT NULL DEFAULT 0
		)`,
	}
}

func postgresSchemaStatements() []string {
	return []string{
		"CREATE TABLE IF NOT EXISTS account_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)",
		`CREATE TABLE IF NOT EXISTS accounts (
			token VARCHAR(512) NOT NULL PRIMARY KEY,
			pool TEXT NOT NULL DEFAULT 'basic',
			status TEXT NOT NULL DEFAULT 'active',
			created_at BIGINT NOT NULL,
			updated_at BIGINT NOT NULL,
			tags TEXT NOT NULL DEFAULT '[]',
			quota_auto TEXT NOT NULL DEFAULT '{}',
			quota_fast TEXT NOT NULL DEFAULT '{}',
			quota_expert TEXT NOT NULL DEFAULT '{}',
			quota_heavy TEXT NOT NULL DEFAULT '{}',
			quota_grok_4_3 TEXT NOT NULL DEFAULT '{}',
			quota_console TEXT NOT NULL DEFAULT '{}',
			usage_use_count INTEGER NOT NULL DEFAULT 0,
			usage_fail_count INTEGER NOT NULL DEFAULT 0,
			usage_sync_count INTEGER NOT NULL DEFAULT 0,
			last_use_at BIGINT,
			last_fail_at BIGINT,
			last_fail_reason TEXT,
			last_sync_at BIGINT,
			last_clear_at BIGINT,
			state_reason TEXT,
			deleted_at BIGINT,
			ext TEXT NOT NULL DEFAULT '{}',
			revision BIGINT NOT NULL DEFAULT 0
		)`,
		"CREATE INDEX IF NOT EXISTS idx_acc_revision ON accounts (revision)",
	}
}

func sqlSeedRevisionSQL(dialect SQLDialect) string {
	if dialect == SQLDialectMySQL {
		return "INSERT INTO account_meta (`key`, value) VALUES ('revision', '0') ON DUPLICATE KEY UPDATE value = '0'"
	}
	return "INSERT INTO account_meta (key, value) VALUES ('revision', '0') ON CONFLICT (key) DO NOTHING"
}

func sqlBumpRevisionSQL(dialect SQLDialect) string {
	if dialect == SQLDialectMySQL {
		return "UPDATE account_meta SET value = CAST(CAST(value AS SIGNED) + 1 AS CHAR) WHERE `key` = ?"
	}
	return "UPDATE account_meta SET value = CAST(CAST(value AS INTEGER) + 1 AS TEXT) WHERE key = " + sqlBind(dialect, 1)
}

const mysqlIndexExistsSQL = `
SELECT COUNT(*)
FROM information_schema.statistics
WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?
`
