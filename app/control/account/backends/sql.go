package backends

import (
	"context"
	"database/sql"
	"errors"
	"sync"

	account "github.com/jiujiu532/grok2api/app/control/account"
)

type SQLDialect string

const (
	SQLDialectMySQL      SQLDialect = "mysql"
	SQLDialectPostgreSQL SQLDialect = "postgresql"
	SQLDialectSQLite     SQLDialect = "sqlite"
)

type SQLAccountRepository struct {
	db          *sql.DB
	dialect     SQLDialect
	closeDB     bool
	cacheKey    string
	initLock    sync.Mutex
	mutationMux sync.Mutex
	initialized bool
}

func NewSQLAccountRepository(db *sql.DB, dialect SQLDialect, closeDB bool) *SQLAccountRepository {
	if dialect == "" {
		dialect = SQLDialectMySQL
	}
	if dialect == SQLDialectSQLite && db != nil {
		db.SetMaxOpenConns(1)
	}
	return &SQLAccountRepository{db: db, dialect: dialect, closeDB: closeDB}
}

func (r *SQLAccountRepository) Initialize(ctx context.Context) error {
	return r.ensureInitialized(ctx)
}

func (r *SQLAccountRepository) GetRevision(ctx context.Context) (int, error) {
	if err := r.ensureInitialized(ctx); err != nil {
		return 0, err
	}
	return getSQLRevision(ctx, r.db, r.dialect)
}

func (r *SQLAccountRepository) Close(context.Context) error {
	if r.cacheKey != "" {
		evictSQLRepositoryCache(r.cacheKey, r)
	}
	if r.closeDB && r.db != nil {
		return r.db.Close()
	}
	return nil
}

func (r *SQLAccountRepository) ensureInitialized(ctx context.Context) error {
	if r.initialized {
		return nil
	}
	r.initLock.Lock()
	defer r.initLock.Unlock()
	if r.initialized {
		return nil
	}
	if r.db == nil {
		return errors.New("sql account repository has nil database")
	}
	if err := ensureSQLSchema(ctx, r.db, r.dialect); err != nil {
		return err
	}
	r.initialized = true
	return nil
}

func newSQLRepository(db *sql.DB, dialect SQLDialect) account.AccountRepository {
	return NewSQLAccountRepository(db, dialect, true)
}
