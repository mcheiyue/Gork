package buildaccount

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dslzl/gork/app/dataplane/build"
	"github.com/dslzl/gork/app/platform/security"
	_ "modernc.org/sqlite"
)

func TestSQLiteStorePlaintextRoundTrip(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "build.db")
	store := NewSQLiteStore(path, nil)
	if err := store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	acc, err := store.Upsert(ctx, FromCredential(build.Credential{
		Name:         "n1",
		Email:        "e@x.com",
		UserID:       "u1",
		AccessToken:  "at-plain",
		RefreshToken: "rt-plain",
		ExpiresAt:    time.Now().UTC().Add(time.Hour),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if acc.ID == 0 {
		t.Fatal("missing id")
	}
	got, err := store.Get(ctx, acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "at-plain" || got.RefreshToken != "rt-plain" || got.UserID != "u1" {
		t.Fatalf("got=%#v", got)
	}
	// cipher 关闭时，列内应为明文（经独立连接读，避开 API 层解密）
	access, refresh := mustRawTokens(t, path, acc.ID)
	if access != "at-plain" || refresh != "rt-plain" {
		t.Fatalf("expected plaintext on disk, access=%q refresh=%q", access, refresh)
	}
}

func TestSQLiteStoreCipherSeal(t *testing.T) {
	ctx := context.Background()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	cipher, err := security.NewCipher(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "build-enc.db")
	store := NewSQLiteStore(path, cipher)
	if err := store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	acc, err := store.Upsert(ctx, Account{
		Name:         "enc",
		UserID:       "u-enc",
		AccessToken:  "secret-access",
		RefreshToken: "secret-refresh",
		Status:       StatusActive,
		ExpiresAt:    time.Now().UTC().Add(2 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	// 磁盘列不得出现明文；应带 gorkenc:v1: 前缀
	access, refresh := mustRawTokens(t, path, acc.ID)
	if strings.Contains(access, "secret-access") || strings.Contains(refresh, "secret-refresh") {
		t.Fatalf("plaintext token leaked to disk: access=%q refresh=%q", access, refresh)
	}
	if !strings.HasPrefix(access, "gorkenc:v1:") || !strings.HasPrefix(refresh, "gorkenc:v1:") {
		t.Fatalf("expected encrypted prefix on disk, access=%q refresh=%q", access, refresh)
	}

	got, err := store.Get(ctx, acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "secret-access" || got.RefreshToken != "secret-refresh" {
		t.Fatalf("decrypt got=%#v", got)
	}

	if err := store.UpdateTokens(ctx, acc.ID, "new-at", "new-rt", time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, err = store.Get(ctx, acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "new-at" || got.RefreshToken != "new-rt" {
		t.Fatalf("updated=%#v", got)
	}
}

func TestSQLiteStoreUpsertByUserIDAndListActive(t *testing.T) {
	ctx := context.Background()
	store := NewSQLiteStore(filepath.Join(t.TempDir(), "b.db"), nil)
	if err := store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	a1, err := store.Upsert(ctx, Account{Name: "a", UserID: "same", AccessToken: "t1", Status: StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	a2, err := store.Upsert(ctx, Account{Name: "a2", UserID: "same", AccessToken: "t2", Status: StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	if a1.ID != a2.ID {
		t.Fatalf("expected upsert same id, %d vs %d", a1.ID, a2.ID)
	}
	got, err := store.GetByUserID(ctx, "same")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "t2" {
		t.Fatalf("got=%#v", got)
	}

	_, _ = store.Upsert(ctx, Account{Name: "dis", UserID: "d1", AccessToken: "x", Status: StatusDisabled})
	active, err := store.ListActive(ctx, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].UserID != "same" {
		t.Fatalf("active=%#v", active)
	}
}

func TestAccountNeedsRefresh(t *testing.T) {
	now := time.Now().UTC()
	a := Account{AccessToken: "at", ExpiresAt: now.Add(30 * time.Second)}
	if !a.NeedsRefresh(now, 2*time.Minute) {
		t.Fatal("expected needs refresh")
	}
	a.ExpiresAt = now.Add(10 * time.Minute)
	if a.NeedsRefresh(now, 2*time.Minute) {
		t.Fatal("expected not yet")
	}
}

// mustRawTokens 用独立只读连接查列值（不经 Cipher），验证落库形态。
// 不读 db 二进制：WAL 模式下主文件可能尚无最新页。
func mustRawTokens(t *testing.T, dbPath string, id int64) (access, refresh string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// 等待 WAL 对共享连接可见
	if _, err := db.Exec(`PRAGMA busy_timeout=5000`); err != nil {
		t.Fatal(err)
	}
	err = db.QueryRow(
		`SELECT access_token, refresh_token FROM build_accounts WHERE id = ?`, id,
	).Scan(&access, &refresh)
	if err != nil {
		t.Fatal(err)
	}
	return access, refresh
}
