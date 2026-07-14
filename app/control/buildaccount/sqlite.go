package buildaccount

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dslzl/gork/app/platform/security"
	_ "modernc.org/sqlite"
)

// SQLiteStore 使用独立 sqlite 文件（默认可与 data 目录并列）。
type SQLiteStore struct {
	path   string
	cipher *security.Cipher
	mu     sync.Mutex
	db     *sql.DB
}

// NewSQLiteStore 创建仓储；cipher 可为 nil（明文落库，兼容默认关闭加密）。
func NewSQLiteStore(dbPath string, cipher *security.Cipher) *SQLiteStore {
	return &SQLiteStore{path: dbPath, cipher: cipher}
}

// Initialize 打开数据库并建表。
func (s *SQLiteStore) Initialize(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil && filepath.Dir(s.path) != "." {
		return err
	}
	db, err := sql.Open("sqlite", s.path)
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.ExecContext(ctx, pragma); err != nil && !strings.Contains(pragma, "journal_mode") {
			db.Close()
			return err
		}
	}
	if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
		db.Close()
		return err
	}
	s.db = db
	return nil
}

func (s *SQLiteStore) dbOrErr() (*sql.DB, error) {
	if s.db == nil {
		return nil, fmt.Errorf("buildaccount store not initialized")
	}
	return s.db, nil
}

// Close 关闭连接。
func (s *SQLiteStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

// Upsert 按 user_id（非空）或 name 去重更新；返回带 ID 的记录。
func (s *SQLiteStore) Upsert(ctx context.Context, account Account) (Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.dbOrErr()
	if err != nil {
		return Account{}, err
	}
	now := time.Now().UTC()
	if account.Status == "" {
		account.Status = StatusActive
	}
	accessEnc, err := security.SealOptional(s.cipher, account.AccessToken)
	if err != nil {
		return Account{}, err
	}
	refreshEnc, err := security.SealOptional(s.cipher, account.RefreshToken)
	if err != nil {
		return Account{}, err
	}

	var existingID int64
	if account.UserID != "" {
		_ = db.QueryRowContext(ctx, `SELECT id FROM build_accounts WHERE user_id = ? AND deleted_at IS NULL`, account.UserID).Scan(&existingID)
	}
	if existingID == 0 && account.ID > 0 {
		existingID = account.ID
	}
	if existingID == 0 && account.Name != "" {
		_ = db.QueryRowContext(ctx, `SELECT id FROM build_accounts WHERE name = ? AND deleted_at IS NULL`, account.Name).Scan(&existingID)
	}

	if existingID > 0 {
		_, err = db.ExecContext(ctx, `
UPDATE build_accounts SET
  name=?, email=?, user_id=?, client_id=?,
  access_token=?, refresh_token=?, expires_at=?,
  status=?, priority=?, updated_at=?
WHERE id=?`,
			account.Name, account.Email, account.UserID, account.ClientID,
			accessEnc, refreshEnc, unixOrNull(account.ExpiresAt),
			account.Status, account.Priority, now.Unix(), existingID,
		)
		if err != nil {
			return Account{}, err
		}
		account.ID = existingID
		account.UpdatedAt = now
		return account, nil
	}

	res, err := db.ExecContext(ctx, `
INSERT INTO build_accounts (
  name, email, user_id, client_id,
  access_token, refresh_token, expires_at,
  status, cooling_until, priority, last_use_at, fail_count,
  created_at, updated_at
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		account.Name, account.Email, account.UserID, account.ClientID,
		accessEnc, refreshEnc, unixOrNull(account.ExpiresAt),
		account.Status, unixOrNull(account.CoolingUntil), account.Priority,
		unixOrNull(account.LastUseAt), account.FailCount,
		now.Unix(), now.Unix(),
	)
	if err != nil {
		return Account{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Account{}, err
	}
	account.ID = id
	account.CreatedAt = now
	account.UpdatedAt = now
	return account, nil
}

// Get 按 id 读取并解密 token。
func (s *SQLiteStore) Get(ctx context.Context, id int64) (Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.dbOrErr()
	if err != nil {
		return Account{}, err
	}
	row := db.QueryRowContext(ctx, selectAccountSQL+` WHERE id = ? AND deleted_at IS NULL`, id)
	return s.scanAccount(row)
}

// GetByUserID 按 user_id 读取。
func (s *SQLiteStore) GetByUserID(ctx context.Context, userID string) (Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.dbOrErr()
	if err != nil {
		return Account{}, err
	}
	row := db.QueryRowContext(ctx, selectAccountSQL+` WHERE user_id = ? AND deleted_at IS NULL`, userID)
	return s.scanAccount(row)
}

// List 全部未删除账号。
func (s *SQLiteStore) List(ctx context.Context) ([]Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.dbOrErr()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, selectAccountSQL+` WHERE deleted_at IS NULL ORDER BY priority DESC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanAccounts(rows)
}

// ListActive 返回 active 且不在冷却中的账号。
func (s *SQLiteStore) ListActive(ctx context.Context, now time.Time) ([]Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.dbOrErr()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, selectAccountSQL+`
WHERE deleted_at IS NULL
  AND status = ?
  AND (cooling_until IS NULL OR cooling_until <= ?)
ORDER BY priority DESC, id ASC`, StatusActive, now.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanAccounts(rows)
}

// UpdateTokens 更新 access/refresh 并可选加密。
func (s *SQLiteStore) UpdateTokens(ctx context.Context, id int64, access, refresh string, expiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.dbOrErr()
	if err != nil {
		return err
	}
	accessEnc, err := security.SealOptional(s.cipher, access)
	if err != nil {
		return err
	}
	refreshEnc, err := security.SealOptional(s.cipher, refresh)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
UPDATE build_accounts SET access_token=?, refresh_token=?, expires_at=?, updated_at=?, status=?
WHERE id=? AND deleted_at IS NULL`,
		accessEnc, refreshEnc, unixOrNull(expiresAt), time.Now().UTC().Unix(), StatusActive, id,
	)
	return err
}

// SetStatus 更新状态。
func (s *SQLiteStore) SetStatus(ctx context.Context, id int64, status string, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.dbOrErr()
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
UPDATE build_accounts SET status=?, state_reason=?, updated_at=? WHERE id=? AND deleted_at IS NULL`,
		status, reason, time.Now().UTC().Unix(), id,
	)
	return err
}

// Delete 软删除。
func (s *SQLiteStore) Delete(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.dbOrErr()
	if err != nil {
		return err
	}
	now := time.Now().UTC().Unix()
	_, err = db.ExecContext(ctx, `UPDATE build_accounts SET deleted_at=?, updated_at=? WHERE id=?`, now, now, id)
	return err
}

type scannable interface {
	Scan(dest ...any) error
}

func (s *SQLiteStore) scanAccount(row scannable) (Account, error) {
	var (
		a                                              Account
		expiresAt, coolingUntil, lastUse, created, upd sql.NullInt64
		accessEnc, refreshEnc                          string
		stateReason                                    sql.NullString
	)
	err := row.Scan(
		&a.ID, &a.Name, &a.Email, &a.UserID, &a.ClientID,
		&accessEnc, &refreshEnc, &expiresAt,
		&a.Status, &coolingUntil, &a.Priority, &lastUse, &a.FailCount,
		&created, &upd, &stateReason,
	)
	if err == sql.ErrNoRows {
		return Account{}, fmt.Errorf("build account not found")
	}
	if err != nil {
		return Account{}, err
	}
	a.AccessToken, err = security.OpenOptional(s.cipher, accessEnc)
	if err != nil {
		return Account{}, err
	}
	a.RefreshToken, err = security.OpenOptional(s.cipher, refreshEnc)
	if err != nil {
		return Account{}, err
	}
	a.ExpiresAt = timeFromUnix(expiresAt)
	a.CoolingUntil = timeFromUnix(coolingUntil)
	a.LastUseAt = timeFromUnix(lastUse)
	a.CreatedAt = timeFromUnix(created)
	a.UpdatedAt = timeFromUnix(upd)
	return a, nil
}

func (s *SQLiteStore) scanAccounts(rows *sql.Rows) ([]Account, error) {
	var out []Account
	for rows.Next() {
		acc, err := s.scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, acc)
	}
	return out, rows.Err()
}

func unixOrNull(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Unix()
}

func timeFromUnix(v sql.NullInt64) time.Time {
	if !v.Valid || v.Int64 <= 0 {
		return time.Time{}
	}
	return time.Unix(v.Int64, 0).UTC()
}

const selectAccountSQL = `
SELECT id, name, email, user_id, client_id,
       access_token, refresh_token, expires_at,
       status, cooling_until, priority, last_use_at, fail_count,
       created_at, updated_at, state_reason
FROM build_accounts`

const schemaSQL = `
CREATE TABLE IF NOT EXISTS build_accounts (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	name            TEXT    NOT NULL DEFAULT '',
	email           TEXT    NOT NULL DEFAULT '',
	user_id         TEXT    NOT NULL DEFAULT '',
	client_id       TEXT    NOT NULL DEFAULT '',
	access_token    TEXT    NOT NULL DEFAULT '',
	refresh_token   TEXT    NOT NULL DEFAULT '',
	expires_at      INTEGER,
	status          TEXT    NOT NULL DEFAULT 'active',
	cooling_until   INTEGER,
	priority        INTEGER NOT NULL DEFAULT 0,
	last_use_at     INTEGER,
	fail_count      INTEGER NOT NULL DEFAULT 0,
	created_at      INTEGER NOT NULL,
	updated_at      INTEGER NOT NULL,
	state_reason    TEXT,
	deleted_at      INTEGER
);
CREATE INDEX IF NOT EXISTS idx_build_acc_status ON build_accounts (status);
CREATE INDEX IF NOT EXISTS idx_build_acc_user ON build_accounts (user_id);
CREATE INDEX IF NOT EXISTS idx_build_acc_deleted ON build_accounts (deleted_at);
`
