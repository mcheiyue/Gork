package backends

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sync"

	account "github.com/jiujiu532/grok2api/app/control/account"
	_ "modernc.org/sqlite"
)

type LocalAccountRepository struct {
	path string
	lock sync.Mutex
}

func NewLocalAccountRepository(dbPath string) *LocalAccountRepository {
	return &LocalAccountRepository{path: dbPath}
}

func (r *LocalAccountRepository) connect(ctx context.Context) (*sql.DB, error) {
	if err := ensureLocalDBParent(r.path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", r.path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := configureLocalDB(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func (r *LocalAccountRepository) Initialize(ctx context.Context) error {
	db, err := r.connect(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	return ensureLocalSchema(ctx, db)
}

func (r *LocalAccountRepository) GetRevision(ctx context.Context) (int, error) {
	db, err := r.connect(ctx)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	return getLocalRevision(ctx, db)
}

func (r *LocalAccountRepository) Close(context.Context) error {
	return nil
}

func configureLocalDB(ctx context.Context, db *sql.DB) error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	}
	for _, pragma := range pragmas {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			return err
		}
	}
	return nil
}

func ensureLocalDBParent(dbPath string) error {
	parent := filepath.Dir(dbPath)
	if parent == "." || parent == "" {
		return nil
	}
	return os.MkdirAll(parent, 0o755)
}

func newLocalRepository(path string) (account.AccountRepository, error) {
	return NewLocalAccountRepository(path), nil
}
