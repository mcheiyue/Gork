package backends

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/pem"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	account "github.com/jiujiu532/grok2api/app/control/account"
	_ "modernc.org/sqlite"
)

func TestSQLAccountRepositoryLifecycle(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLRepository(t)

	if err := repo.Initialize(ctx); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	revision, err := repo.GetRevision(ctx)
	if err != nil || revision != 0 {
		t.Fatalf("initial revision = %d/%v, want 0/nil", revision, err)
	}

	upserted, err := repo.UpsertAccounts(ctx, []account.AccountUpsert{
		{Token: "sso= tok-a", Pool: "super", Tags: []string{"z", "b"}, Ext: map[string]any{"keep": "yes"}},
	})
	if err != nil || upserted.Upserted != 1 || upserted.Revision != 1 {
		t.Fatalf("UpsertAccounts = %#v/%v, want 1 rev 1", upserted, err)
	}

	failDelta, useDelta, syncDelta := 3, -5, 2
	lastFailAt, lastSyncAt := int64(111), int64(222)
	failReason := "bad"
	disabled := account.AccountStatusDisabled
	patched, err := repo.PatchAccounts(ctx, []account.AccountPatch{{
		Token:          "tok-a",
		Status:         &disabled,
		AddTags:        []string{"a"},
		RemoveTags:     []string{"b"},
		UsageUseDelta:  &useDelta,
		UsageFailDelta: &failDelta,
		UsageSyncDelta: &syncDelta,
		LastFailAt:     &lastFailAt,
		LastFailReason: &failReason,
		LastSyncAt:     &lastSyncAt,
		ExtMerge:       map[string]any{"disabled_at": float64(1), "merged": "ok"},
		ClearFailures:  true,
		QuotaConsole:   map[string]any{"remaining": float64(9), "total": float64(10)},
		QuotaGrok43:    map[string]any{"remaining": float64(4), "total": float64(5)},
		QuotaHeavy:     map[string]any{"remaining": float64(1), "total": float64(2)},
		QuotaAuto:      map[string]any{"remaining": float64(7), "total": float64(8)},
		QuotaFast:      map[string]any{"remaining": float64(6), "total": float64(7)},
		QuotaExpert:    map[string]any{"remaining": float64(5), "total": float64(6)},
	}})
	if err != nil || patched.Patched != 1 || patched.Revision != 2 {
		t.Fatalf("PatchAccounts = %#v/%v, want 1 rev 2", patched, err)
	}

	records, err := repo.GetAccounts(ctx, []string{"tok-a"})
	if err != nil || len(records) != 1 {
		t.Fatalf("GetAccounts = %#v/%v, want one record", records, err)
	}
	record := records[0]
	if record.Token != "tok-a" || record.Status != account.AccountStatusActive {
		t.Fatalf("patched record identity/status = %#v", record)
	}
	if record.UsageUseCount != 0 || record.UsageFailCount != 0 || record.UsageSyncCount != 2 {
		t.Fatalf("usage counters = %d/%d/%d", record.UsageUseCount, record.UsageFailCount, record.UsageSyncCount)
	}
	if record.LastFailAt != nil || record.LastFailReason != nil || record.StateReason != nil {
		t.Fatalf("failure state was not cleared: %#v", record)
	}
	if got := record.Tags; len(got) != 2 || got[0] != "z" || got[1] != "a" {
		t.Fatalf("tags = %#v, want [z a]", got)
	}
	if _, ok := record.Ext["disabled_at"]; ok || record.Ext["merged"] != "ok" || record.Ext["keep"] != "yes" {
		t.Fatalf("ext merge/clear = %#v", record.Ext)
	}
	if _, ok := record.Quota["grok_4_3"]; !ok {
		t.Fatalf("quota missing grok_4_3: %#v", record.Quota)
	}
	if _, ok := record.Quota["console"]; !ok {
		t.Fatalf("quota missing console: %#v", record.Quota)
	}

	changes, err := repo.ScanChanges(ctx, 1, 10)
	if err != nil || changes.Revision != 2 || len(changes.Items) != 1 || len(changes.DeletedTokens) != 0 {
		t.Fatalf("ScanChanges after patch = %#v/%v", changes, err)
	}
	deleted, err := repo.DeleteAccounts(ctx, []string{"tok-a"})
	if err != nil || deleted.Deleted != 1 || deleted.Revision != 3 {
		t.Fatalf("DeleteAccounts = %#v/%v, want 1 rev 3", deleted, err)
	}
	changes, err = repo.ScanChanges(ctx, 2, 10)
	if err != nil || len(changes.Items) != 0 || len(changes.DeletedTokens) != 1 || changes.DeletedTokens[0] != "tok-a" {
		t.Fatalf("ScanChanges after delete = %#v/%v", changes, err)
	}
}

func TestSQLAccountRepositoryListAndReplacePool(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLRepository(t)
	if err := repo.Initialize(ctx); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	_, err := repo.UpsertAccounts(ctx, []account.AccountUpsert{
		{Token: "basic-a", Pool: "basic", Tags: []string{"x"}},
		{Token: "basic-b", Pool: "basic", Tags: []string{"x", "drop"}},
		{Token: "super-a", Pool: "super", Tags: []string{"y"}},
	})
	if err != nil {
		t.Fatalf("UpsertAccounts returned error: %v", err)
	}

	pool := "basic"
	page, err := repo.ListAccounts(ctx, account.ListAccountsQuery{
		Page:        1,
		PageSize:    10,
		Pool:        &pool,
		Tags:        []string{"x"},
		ExcludeTags: []string{"drop"},
		SortBy:      "token",
	})
	if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].Token != "basic-a" {
		t.Fatalf("ListAccounts filtered page = %#v/%v", page, err)
	}

	replaced, err := repo.ReplacePool(ctx, account.BulkReplacePoolCommand{
		Pool: "basic",
		Upserts: []account.AccountUpsert{
			{Token: "basic-new", Pool: "basic", Tags: []string{"x"}},
		},
	})
	if err != nil || replaced.Upserted != 1 || replaced.Deleted != 2 || replaced.Revision != 3 {
		t.Fatalf("ReplacePool = %#v/%v, want 1 upsert 2 deleted rev 3", replaced, err)
	}
	snapshot, err := repo.RuntimeSnapshot(ctx)
	if err != nil {
		t.Fatalf("RuntimeSnapshot returned error: %v", err)
	}
	tokens := map[string]bool{}
	for _, item := range snapshot.Items {
		tokens[item.Token] = true
	}
	if len(tokens) != 2 || !tokens["basic-new"] || !tokens["super-a"] {
		t.Fatalf("snapshot tokens after replace = %#v", tokens)
	}
}

func TestSQLDriverURLsAndFactoryDefaults(t *testing.T) {
	mysqlDSN, err := normalizeMySQLDSN("mariadb://user:pass@db.example:3307/name?tls=false&parseTime=true")
	if err != nil {
		t.Fatalf("normalizeMySQLDSN returned error: %v", err)
	}
	if !strings.Contains(mysqlDSN, "user:pass@tcp(db.example:3307)/name") || !strings.Contains(mysqlDSN, "tls=false") {
		t.Fatalf("mysql dsn = %q", mysqlDSN)
	}

	pgURL := normalizePostgreSQLURL("pgsql://user:pass@db.example:5432/name?sslmode=require")
	if !strings.HasPrefix(pgURL, "postgres://user:pass@db.example:5432/name") {
		t.Fatalf("postgres url = %q", pgURL)
	}

	constructors := RepositoryConstructors{}.WithDefaults()
	if _, err := constructors.MySQL(""); err == nil || strings.Contains(err.Error(), "not migrated") {
		t.Fatalf("MySQL default constructor still reports not migrated: %v", err)
	}
	if _, err := constructors.PostgreSQL(""); err == nil || strings.Contains(err.Error(), "not migrated") {
		t.Fatalf("PostgreSQL default constructor still reports not migrated: %v", err)
	}
}

func TestSQLDriverPostgreSQLSSLURLParity(t *testing.T) {
	caPath, certPath, keyPath := writeSQLTestTLSFiles(t)
	rawURL := "postgresql://user:pass@db.example:5432/name?sslmode=verify-identity" +
		"&sslrootcert=" + url.QueryEscape(caPath) +
		"&sslcert=" + url.QueryEscape(certPath) +
		"&sslkey=" + url.QueryEscape(keyPath) +
		"&connect_timeout=5"
	pgURL, err := preparePostgreSQLURL(rawURL)
	if err != nil {
		t.Fatalf("preparePostgreSQLURL returned error: %v", err)
	}
	for _, expected := range []string{"sslmode=verify-full", "sslrootcert=" + url.QueryEscape(caPath), "sslcert=" + url.QueryEscape(certPath), "sslkey=" + url.QueryEscape(keyPath), "connect_timeout=5"} {
		if !strings.Contains(pgURL, expected) {
			t.Fatalf("postgres url missing %q: %s", expected, pgURL)
		}
	}

	if _, err := preparePostgreSQLURL("postgres://u:p@db/name?sslrootcert=/ca.pem"); err == nil || !strings.Contains(err.Error(), "require sslmode") {
		t.Fatalf("postgres sslrootcert without sslmode error = %v, want require sslmode", err)
	}
	if _, err := preparePostgreSQLURL("postgres://u:p@db/name?sslmode=disable&sslcert=/client.pem"); err == nil || !strings.Contains(err.Error(), "cannot be used with sslmode=disable") {
		t.Fatalf("postgres disable+cert error = %v", err)
	}
	if _, err := preparePostgreSQLURL("postgres://u:p@db/name?sslmode=require&sslpassword=secret"); err == nil || !strings.Contains(err.Error(), "Unsupported PostgreSQL SSL URL parameter") {
		t.Fatalf("postgres unsupported ssl parameter error = %v", err)
	}
	if _, err := preparePostgreSQLURL("postgres://u:p@db/name?sslmode=bogus"); err == nil || !strings.Contains(err.Error(), "Unsupported SSL mode") {
		t.Fatalf("postgres unsupported sslmode error = %v", err)
	}
	if _, err := preparePostgreSQLURL("postgres://u:p@db/name?sslmode=require&sslkey=/client.key"); err == nil || !strings.Contains(err.Error(), "sslkey requires sslcert") {
		t.Fatalf("postgres sslkey without sslcert error = %v", err)
	}
	if _, err := preparePostgreSQLURL("postgres://u:p@db/name?sslmode=require&sslrootcert=/definitely/missing/ca.pem"); err == nil {
		t.Fatalf("postgres missing sslrootcert returned nil error")
	}
}

func TestSQLDriverMySQLSSLURLParity(t *testing.T) {
	caPath, certPath, keyPath := writeSQLTestTLSFiles(t)
	rawURL := "mysql://user:pass@db.example/name?ssl-mode=verify-identity" +
		"&ssl-ca=" + url.QueryEscape(caPath) +
		"&ssl-cert=" + url.QueryEscape(certPath) +
		"&ssl-key=" + url.QueryEscape(keyPath) +
		"&ssl-check-hostname=false&ssl-cipher=ECDHE-RSA-AES128-GCM-SHA256&parseTime=true"
	mysqlDSN, err := prepareMySQLDSN(rawURL)
	if err != nil {
		t.Fatalf("prepareMySQLDSN returned error: %v", err)
	}
	for _, unexpected := range []string{"ssl-ca", "ssl-cert", "ssl-key", "ssl-check-hostname", "ssl-cipher"} {
		if strings.Contains(mysqlDSN, unexpected) {
			t.Fatalf("mysql dsn still contains stripped SSL parameter %q: %s", unexpected, mysqlDSN)
		}
	}
	for _, expected := range []string{"tls=grok2api_", "parseTime=true"} {
		if !strings.Contains(mysqlDSN, expected) {
			t.Fatalf("mysql dsn missing %q: %s", expected, mysqlDSN)
		}
	}

	if _, err := prepareMySQLDSN("mysql://u:p@db/name?ssl-ca=/ca.pem"); err == nil || !strings.Contains(err.Error(), "require ssl-mode") {
		t.Fatalf("mysql ssl-ca without ssl-mode error = %v, want require ssl-mode", err)
	}
	if _, err := prepareMySQLDSN("mysql://u:p@db/name?ssl-mode=disabled&ssl-cert=/client.pem"); err == nil || !strings.Contains(err.Error(), "cannot be used with ssl-mode=disabled") {
		t.Fatalf("mysql disabled+cert error = %v", err)
	}
	if _, err := prepareMySQLDSN("mysql://u:p@db/name?ssl-mode=prefer"); err == nil || !strings.Contains(err.Error(), "allow/prefer is not supported") {
		t.Fatalf("mysql prefer error = %v", err)
	}
	if _, err := prepareMySQLDSN("mysql://u:p@db/name?ssl-mode=required&ssl-check-hostname=maybe"); err == nil || !strings.Contains(err.Error(), "Unsupported boolean value") {
		t.Fatalf("mysql invalid ssl-check-hostname error = %v", err)
	}
	if _, err := prepareMySQLDSN("mysql://u:p@db/name?ssl-mode=required&ssl-key=/client.key"); err == nil || !strings.Contains(err.Error(), "ssl-key requires ssl-cert") {
		t.Fatalf("mysql ssl-key without ssl-cert error = %v", err)
	}
	if _, err := prepareMySQLDSN("mysql://u:p@db/name?ssl-mode=verify-identity&ssl-ca=/definitely/missing/ca.pem"); err == nil {
		t.Fatalf("mysql missing ssl-ca returned nil error")
	}

	tlsConfig, err := buildMySQLTLSConfig("verify_identity", map[string]string{"ssl-cipher": "ECDHE-RSA-AES128-GCM-SHA256"}, "db.example", nil)
	if err != nil {
		t.Fatalf("buildMySQLTLSConfig with cipher returned error: %v", err)
	}
	if len(tlsConfig.CipherSuites) != 1 || tlsConfig.CipherSuites[0] != tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256 {
		t.Fatalf("CipherSuites = %#v, want TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256", tlsConfig.CipherSuites)
	}
	if _, err := buildMySQLTLSConfig("verify_identity", map[string]string{"ssl-cipher": "NOT-A-CIPHER"}, "db.example", nil); err == nil || !strings.Contains(err.Error(), "Unsupported MySQL ssl-cipher") {
		t.Fatalf("invalid ssl-cipher error = %v", err)
	}
	verifyCAConfig, err := buildMySQLTLSConfig("verify_ca", map[string]string{}, "db.example", nil)
	if err != nil {
		t.Fatalf("buildMySQLTLSConfig verify_ca returned error: %v", err)
	}
	if !verifyCAConfig.InsecureSkipVerify || verifyCAConfig.VerifyPeerCertificate == nil {
		t.Fatalf("verify_ca config should skip hostname but keep certificate-chain verification")
	}
	disableHostname := false
	verifyIdentityNoHostname, err := buildMySQLTLSConfig("verify_identity", map[string]string{}, "db.example", &disableHostname)
	if err != nil {
		t.Fatalf("buildMySQLTLSConfig verify_identity without hostname returned error: %v", err)
	}
	if !verifyIdentityNoHostname.InsecureSkipVerify || verifyIdentityNoHostname.VerifyPeerCertificate == nil {
		t.Fatalf("verify_identity with ssl-check-hostname=false should still verify certificate chain")
	}
}

func TestSQLRepositoryConstructorCacheEvictsOnClose(t *testing.T) {
	rawURL := "postgres://user:pass@db.example/name?sslmode=disable"
	first, err := getOrCreateSQLRepository(SQLDialectPostgreSQL, rawURL, func(string) (*sql.DB, error) {
		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			return nil, err
		}
		return db, nil
	})
	if err != nil {
		t.Fatalf("getOrCreateSQLRepository first returned error: %v", err)
	}
	second, err := getOrCreateSQLRepository(SQLDialectPostgreSQL, rawURL, func(string) (*sql.DB, error) {
		t.Fatal("factory called for cached repository")
		return nil, nil
	})
	if err != nil {
		t.Fatalf("getOrCreateSQLRepository second returned error: %v", err)
	}
	if first != second {
		t.Fatalf("cached repository mismatch: %p != %p", first, second)
	}
	if err := first.Close(context.Background()); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	third, err := getOrCreateSQLRepository(SQLDialectPostgreSQL, rawURL, func(string) (*sql.DB, error) {
		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			return nil, err
		}
		return db, nil
	})
	if err != nil {
		t.Fatalf("getOrCreateSQLRepository third returned error: %v", err)
	}
	defer third.Close(context.Background())
	if third == first {
		t.Fatalf("repository cache was not evicted after close")
	}
}

func writeSQLTestTLSFiles(t *testing.T) (string, string, string) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey returned error: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "db.example"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		IsCA:         true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("x509.CreateCertificate returned error: %v", err)
	}
	dir := t.TempDir()
	certPath := filepath.Join(dir, "client.pem")
	keyPath := filepath.Join(dir, "client.key")
	caPath := filepath.Join(dir, "ca.pem")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	for _, item := range []struct {
		path string
		data []byte
	}{
		{caPath, certPEM},
		{certPath, certPEM},
		{keyPath, keyPEM},
	} {
		if err := os.WriteFile(item.path, item.data, 0o600); err != nil {
			t.Fatalf("os.WriteFile(%s) returned error: %v", item.path, err)
		}
	}
	return caPath, certPath, keyPath
}

func TestConfigureSQLPoolUsesEnvAndServerlessDefaults(t *testing.T) {
	t.Setenv("ACCOUNT_SQL_POOL_SIZE", "4")
	t.Setenv("ACCOUNT_SQL_MAX_OVERFLOW", "6")
	t.Setenv("ACCOUNT_SQL_POOL_RECYCLE", "0")
	t.Setenv("VERCEL", "")
	t.Setenv("AWS_LAMBDA_FUNCTION_NAME", "")
	t.Setenv("FUNCTIONS_WORKER_RUNTIME", "")

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	defer db.Close()
	configureSQLPool(db)
	if got := db.Stats().MaxOpenConnections; got != 10 {
		t.Fatalf("MaxOpenConnections = %d, want 10", got)
	}

	t.Setenv("ACCOUNT_SQL_POOL_SIZE", "")
	t.Setenv("ACCOUNT_SQL_MAX_OVERFLOW", "")
	t.Setenv("VERCEL", "1")
	serverlessDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open serverless returned error: %v", err)
	}
	defer serverlessDB.Close()
	configureSQLPool(serverlessDB)
	if got := serverlessDB.Stats().MaxOpenConnections; got != 3 {
		t.Fatalf("serverless MaxOpenConnections = %d, want 3", got)
	}
}

func newTestSQLRepository(t *testing.T) *SQLAccountRepository {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return NewSQLAccountRepository(db, SQLDialectSQLite, false)
}
