package startup

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRunStartupMigrationsMigratesConfigAndBasicInterval(t *testing.T) {
	dir := t.TempDir()
	userConfig := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(userConfig, []byte("user = true\n"), 0o644); err != nil {
		t.Fatalf("write user config: %v", err)
	}

	config := &fakeMigrationConfig{
		version: int64(0),
		data: map[string]any{
			"account": map[string]any{
				"refresh": map[string]any{"basic_interval_sec": int64(36000)},
			},
		},
	}
	repo := &fakeMigrationRepo{}

	err := RunStartupMigrations(context.Background(), config, repo, StartupMigrationOptions{
		ConfigBackendName:     "redis",
		RepositoryBackendName: "local",
		UserConfigPath:        userConfig,
		LoadTOML: func(path string) (map[string]any, error) {
			if path != userConfig {
				t.Fatalf("LoadTOML path = %q", path)
			}
			return map[string]any{"feature": map[string]any{"enabled": true}}, nil
		},
	})
	if err != nil {
		t.Fatalf("RunStartupMigrations returned error: %v", err)
	}

	if len(config.patches) != 2 {
		t.Fatalf("patches=%#v", config.patches)
	}
	if !reflect.DeepEqual(config.patches[0], map[string]any{"feature": map[string]any{"enabled": true}}) {
		t.Fatalf("config migration patch = %#v", config.patches[0])
	}
	wantInterval := map[string]any{"account": map[string]any{"refresh": map[string]any{"basic_interval_sec": 86400}}}
	if !reflect.DeepEqual(config.patches[1], wantInterval) {
		t.Fatalf("basic interval patch = %#v", config.patches[1])
	}
}

func TestRunStartupMigrationsSeedsLocalConfigFromDefaultPath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	dataDir := filepath.Join(dir, "custom-data")
	t.Setenv("DATA_DIR", dataDir)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("make data dir: %v", err)
	}
	if err := os.WriteFile("config.defaults.toml", []byte("default = true\n"), 0o644); err != nil {
		t.Fatalf("write defaults: %v", err)
	}

	var copiedFrom, copiedTo string
	err := RunStartupMigrations(context.Background(), &fakeMigrationConfig{}, &fakeMigrationRepo{}, StartupMigrationOptions{
		ConfigBackendName:     "local",
		RepositoryBackendName: "local",
		CopyFile: func(from, to string) error {
			copiedFrom, copiedTo = from, to
			return nil
		},
	})
	if err != nil {
		t.Fatalf("RunStartupMigrations returned error: %v", err)
	}
	if copiedFrom != "config.defaults.toml" || copiedTo != filepath.Join(dataDir, "config.toml") {
		t.Fatalf("copy %q -> %q", copiedFrom, copiedTo)
	}
}

func TestMigrateConfigSkipsNonEmptyBackendAndMissingUserConfig(t *testing.T) {
	dir := t.TempDir()
	userConfig := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(userConfig, []byte("user = true\n"), 0o644); err != nil {
		t.Fatalf("write user config: %v", err)
	}

	config := &fakeMigrationConfig{version: 7}
	err := migrateConfig(context.Background(), config, StartupMigrationOptions{
		ConfigBackendName: "redis",
		UserConfigPath:    userConfig,
		LoadTOML: func(string) (map[string]any, error) {
			t.Fatalf("LoadTOML should not be called for non-empty backend")
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("migrateConfig non-empty backend returned error: %v", err)
	}
	if len(config.patches) != 0 {
		t.Fatalf("non-empty backend patches=%#v", config.patches)
	}

	config = &fakeMigrationConfig{version: 0}
	err = migrateConfig(context.Background(), config, StartupMigrationOptions{
		ConfigBackendName: "redis",
		UserConfigPath:    filepath.Join(dir, "missing.toml"),
		LoadTOML: func(string) (map[string]any, error) {
			t.Fatalf("LoadTOML should not be called without user config")
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("migrateConfig missing config returned error: %v", err)
	}
	if len(config.patches) != 0 {
		t.Fatalf("missing config patches=%#v", config.patches)
	}
}

func TestMigrateAccountsCopiesEmptyTargetAndRenamesSQLite(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "accounts.db")
	if err := os.WriteFile(dbPath, []byte("sqlite"), 0o644); err != nil {
		t.Fatalf("write sqlite marker: %v", err)
	}

	source := &fakeLocalAccountSource{pages: []ListAccountsResult{{
		Items: []AccountRecord{
			{
				Token: "tok-1", Pool: "super", Status: "active",
				Tags: []string{"a"}, Ext: map[string]any{"x": "y"},
				Quotas:        AccountQuotaSet{Auto: map[string]any{"limit": 1}},
				UsageUseCount: 3, DeletedAt: "deleted",
			},
		},
		TotalPages: 1,
	}}}
	repo := &fakeMigrationRepo{snapshot: AccountRuntimeSnapshot{}}
	var renamedFrom, renamedTo string

	err := migrateAccounts(context.Background(), repo, StartupMigrationOptions{
		RepositoryBackendName: "redis",
		LocalDBPath:           dbPath,
		LocalSourceFactory: func(path string) (LocalAccountSource, error) {
			if path != dbPath {
				t.Fatalf("source path = %q", path)
			}
			return source, nil
		},
		Rename: func(from, to string) error {
			renamedFrom, renamedTo = from, to
			return nil
		},
	})
	if err != nil {
		t.Fatalf("migrateAccounts returned error: %v", err)
	}

	if len(repo.upserts) != 1 || repo.upserts[0].Token != "tok-1" || repo.upserts[0].Pool != "super" {
		t.Fatalf("upserts=%#v", repo.upserts)
	}
	if len(repo.patches) != 1 || repo.patches[0].Token != "tok-1" ||
		repo.patches[0].QuotaAuto["limit"] != 1 || repo.patches[0].UsageUseDelta == nil ||
		*repo.patches[0].UsageUseDelta != 3 {
		t.Fatalf("patches=%#v", repo.patches)
	}
	if !reflect.DeepEqual(repo.deleted, []string{"tok-1"}) {
		t.Fatalf("deleted=%#v", repo.deleted)
	}
	if !source.closed || renamedFrom != dbPath || renamedTo != filepath.Join(dir, "accounts.db.migrated") {
		t.Fatalf("closed=%t renamed=%q -> %q", source.closed, renamedFrom, renamedTo)
	}
}

func TestMigrateAccountsRenamesSQLiteWhenLocalSourceIsEmpty(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "accounts.db")
	if err := os.WriteFile(dbPath, []byte("sqlite"), 0o644); err != nil {
		t.Fatalf("write sqlite marker: %v", err)
	}

	source := &fakeLocalAccountSource{pages: []ListAccountsResult{{TotalPages: 1}}}
	repo := &fakeMigrationRepo{snapshot: AccountRuntimeSnapshot{}}
	var renamedFrom, renamedTo string

	err := migrateAccounts(context.Background(), repo, StartupMigrationOptions{
		RepositoryBackendName: "redis",
		LocalDBPath:           dbPath,
		LocalSourceFactory: func(path string) (LocalAccountSource, error) {
			if path != dbPath {
				t.Fatalf("source path = %q", path)
			}
			return source, nil
		},
		Rename: func(from, to string) error {
			renamedFrom, renamedTo = from, to
			return nil
		},
	})
	if err != nil {
		t.Fatalf("migrateAccounts returned error: %v", err)
	}
	if !source.closed || renamedFrom != dbPath || renamedTo != filepath.Join(dir, "accounts.db.migrated") {
		t.Fatalf("closed=%t renamed=%q -> %q", source.closed, renamedFrom, renamedTo)
	}
}

func TestRunStartupMigrationsUsesDataDirForDefaultAccountsDB(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "custom-data")
	t.Setenv("DATA_DIR", dataDir)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("make data dir: %v", err)
	}
	dbPath := filepath.Join(dataDir, "accounts.db")
	if err := os.WriteFile(dbPath, []byte("sqlite"), 0o644); err != nil {
		t.Fatalf("write sqlite marker: %v", err)
	}

	source := &fakeLocalAccountSource{pages: []ListAccountsResult{{TotalPages: 1}}}
	repo := &fakeMigrationRepo{snapshot: AccountRuntimeSnapshot{}}
	var sourcePath, renamedFrom, renamedTo string

	err := RunStartupMigrations(context.Background(), &fakeMigrationConfig{}, repo, StartupMigrationOptions{
		ConfigBackendName:     "redis",
		RepositoryBackendName: "redis",
		LocalSourceFactory: func(path string) (LocalAccountSource, error) {
			sourcePath = path
			return source, nil
		},
		Rename: func(from, to string) error {
			renamedFrom, renamedTo = from, to
			return nil
		},
	})
	if err != nil {
		t.Fatalf("RunStartupMigrations returned error: %v", err)
	}
	if sourcePath != dbPath || renamedFrom != dbPath || renamedTo != filepath.Join(dataDir, "accounts.db.migrated") {
		t.Fatalf("source=%q renamed=%q -> %q", sourcePath, renamedFrom, renamedTo)
	}
}

func TestQuotaBackfillsPatchExpectedAccounts(t *testing.T) {
	config := &fakeMigrationConfig{data: map[string]any{}}
	repo := &fakeMigrationRepo{pages: []ListAccountsResult{{
		Items: []AccountRecord{
			{Token: "super-missing", Pool: "super", Quotas: AccountQuotaSet{}},
			{Token: "super-present", Pool: "super", Quotas: AccountQuotaSet{Grok43: map[string]any{"limit": 4}, Console: map[string]any{"limit": 5}}},
			{Token: "basic-needs-normalize", Pool: "basic", Quotas: AccountQuotaSet{Auto: map[string]any{"limit": 1}}},
			{Token: "console-missing", Pool: "pro", Quotas: AccountQuotaSet{}},
		},
		TotalPages: 1,
	}}}
	options := StartupMigrationOptions{
		DefaultQuotaWindow: func(pool string, kind int) map[string]any {
			return map[string]any{"pool": pool, "kind": kind}
		},
		NormalizeQuotaSet: func(pool string, quotas AccountQuotaSet) AccountQuotaSet {
			if pool == "basic" && quotas.Fast == nil {
				quotas.Fast = map[string]any{"only": "fast"}
			}
			return quotas
		},
	}

	if err := backfillGrok43Quota(context.Background(), config, repo, options); err != nil {
		t.Fatalf("backfillGrok43Quota returned error: %v", err)
	}
	if err := normalizeBasicFastOnlyQuota(context.Background(), repo, options); err != nil {
		t.Fatalf("normalizeBasicFastOnlyQuota returned error: %v", err)
	}
	if err := backfillConsoleQuota(context.Background(), repo, options); err != nil {
		t.Fatalf("backfillConsoleQuota returned error: %v", err)
	}

	grokPatch := findPatch(repo.patches, "super-missing", func(p AccountPatch) bool { return p.QuotaGrok43 != nil })
	if grokPatch.QuotaGrok43["kind"] != 4 {
		t.Fatalf("missing grok patch: %#v", grokPatch)
	}
	basicPatch := findPatch(repo.patches, "basic-needs-normalize", func(p AccountPatch) bool { return p.QuotaFast != nil })
	if basicPatch.QuotaFast["only"] != "fast" {
		t.Fatalf("missing basic normalize patch: %#v", basicPatch)
	}
	consolePatch := findPatch(repo.patches, "console-missing", func(p AccountPatch) bool { return p.QuotaConsole != nil })
	if consolePatch.QuotaConsole["kind"] != 5 {
		t.Fatalf("missing console patch: %#v", consolePatch)
	}
	if patch := findPatch(repo.patches, "super-present", func(AccountPatch) bool { return true }); patch.Token != "" {
		t.Fatalf("present quotas should not be patched: %#v", patch)
	}
}

func TestGrok43BackfillQueriesOnlyEligiblePoolsAndMarksComplete(t *testing.T) {
	config := &fakeMigrationConfig{data: map[string]any{}}
	repo := &fakeMigrationRepo{pages: []ListAccountsResult{{
		Items: []AccountRecord{
			{Token: "super-missing", Pool: "super", Quotas: AccountQuotaSet{}},
			{Token: "heavy-missing", Pool: "heavy", Quotas: AccountQuotaSet{}},
			{Token: "basic-ignored", Pool: "basic", Quotas: AccountQuotaSet{}},
		},
		TotalPages: 1,
	}}}
	options := StartupMigrationOptions{
		DefaultQuotaWindow: func(pool string, kind int) map[string]any {
			return map[string]any{"pool": pool, "kind": kind}
		},
	}

	if err := backfillGrok43Quota(context.Background(), config, repo, options); err != nil {
		t.Fatalf("backfillGrok43Quota returned error: %v", err)
	}

	if got := migrationQueryPools(repo.queries); !reflect.DeepEqual(got, []string{"super", "heavy"}) {
		t.Fatalf("query pools = %#v", got)
	}
	if patch := findPatch(repo.patches, "basic-ignored", func(AccountPatch) bool { return true }); patch.Token != "" {
		t.Fatalf("basic account should not be patched: %#v", patch)
	}
	if !fakeConfigBool(config.data, "startup", "migrations", "grok43_quota_backfill") {
		t.Fatalf("migration completion marker missing: %#v", config.data)
	}
}

func TestGrok43BackfillSkipsWhenMarkedComplete(t *testing.T) {
	config := &fakeMigrationConfig{data: map[string]any{
		"startup": map[string]any{
			"migrations": map[string]any{"grok43_quota_backfill": true},
		},
	}}
	repo := &fakeMigrationRepo{pages: []ListAccountsResult{{
		Items:      []AccountRecord{{Token: "super-missing", Pool: "super", Quotas: AccountQuotaSet{}}},
		TotalPages: 1,
	}}}

	if err := backfillGrok43Quota(context.Background(), config, repo, StartupMigrationOptions{
		DefaultQuotaWindow: func(pool string, kind int) map[string]any {
			return map[string]any{"pool": pool, "kind": kind}
		},
	}); err != nil {
		t.Fatalf("backfillGrok43Quota returned error: %v", err)
	}

	if len(repo.queries) != 0 {
		t.Fatalf("marked migration should not query accounts: %#v", repo.queries)
	}
	if len(repo.patches) != 0 {
		t.Fatalf("marked migration should not patch accounts: %#v", repo.patches)
	}
}

func TestCountKeysMatchesPythonRecursiveHelper(t *testing.T) {
	got := countKeys(map[string]any{
		"server": map[string]any{
			"host": "0.0.0.0",
			"port": 8000,
		},
		"account": map[string]any{
			"refresh": map[string]any{
				"basic_interval_sec": 86400,
			},
			"empty": map[string]any{},
		},
	})
	if got != 3 {
		t.Fatalf("countKeys() = %d, want 3", got)
	}
}

func findPatch(patches []AccountPatch, token string, match func(AccountPatch) bool) AccountPatch {
	for _, patch := range patches {
		if patch.Token == token && match(patch) {
			return patch
		}
	}
	return AccountPatch{}
}

type fakeMigrationConfig struct {
	version any
	data    map[string]any
	patches []map[string]any
}

func (c *fakeMigrationConfig) Load(context.Context) (map[string]any, error) { return c.data, nil }
func (c *fakeMigrationConfig) Version(context.Context) (any, error)         { return c.version, nil }
func (c *fakeMigrationConfig) ApplyPatch(_ context.Context, patch map[string]any) error {
	c.patches = append(c.patches, patch)
	c.data = mergeFakeConfigPatch(c.data, patch)
	return nil
}

type fakeMigrationRepo struct {
	snapshot AccountRuntimeSnapshot
	pages    []ListAccountsResult
	queries  []ListAccountsQuery
	upserts  []AccountUpsert
	patches  []AccountPatch
	deleted  []string
}

func (r *fakeMigrationRepo) RuntimeSnapshot(context.Context) (AccountRuntimeSnapshot, error) {
	return r.snapshot, nil
}

func (r *fakeMigrationRepo) ListAccounts(_ context.Context, query ListAccountsQuery) (ListAccountsResult, error) {
	r.queries = append(r.queries, query)
	if len(r.pages) == 0 {
		return ListAccountsResult{TotalPages: 1}, nil
	}
	result := r.pages[0]
	items := make([]AccountRecord, 0, len(result.Items))
	for _, item := range result.Items {
		if query.Pool != "" && item.Pool != query.Pool {
			continue
		}
		if !query.IncludeDeleted && item.DeletedAt != nil {
			continue
		}
		items = append(items, item)
	}
	result.Items = items
	return result, nil
}

func (r *fakeMigrationRepo) UpsertAccounts(_ context.Context, upserts []AccountUpsert) (AccountPatchResult, error) {
	r.upserts = append(r.upserts, upserts...)
	return AccountPatchResult{Patched: len(upserts)}, nil
}

func (r *fakeMigrationRepo) PatchAccounts(_ context.Context, patches []AccountPatch) (AccountPatchResult, error) {
	r.patches = append(r.patches, patches...)
	return AccountPatchResult{Patched: len(patches)}, nil
}

func (r *fakeMigrationRepo) DeleteAccounts(_ context.Context, tokens []string) (AccountPatchResult, error) {
	r.deleted = append(r.deleted, tokens...)
	return AccountPatchResult{Patched: len(tokens)}, nil
}

type fakeLocalAccountSource struct {
	pages  []ListAccountsResult
	closed bool
}

func (s *fakeLocalAccountSource) ListAccounts(_ context.Context, query ListAccountsQuery) (ListAccountsResult, error) {
	if len(s.pages) == 0 {
		return ListAccountsResult{TotalPages: 1}, nil
	}
	return s.pages[0], nil
}

func (s *fakeLocalAccountSource) Close(context.Context) error {
	s.closed = true
	return nil
}

func migrationQueryPools(queries []ListAccountsQuery) []string {
	pools := make([]string, 0, len(queries))
	for _, query := range queries {
		pools = append(pools, query.Pool)
	}
	return pools
}

func fakeConfigBool(data map[string]any, keys ...string) bool {
	var current any = data
	for _, key := range keys {
		asMap, ok := current.(map[string]any)
		if !ok {
			return false
		}
		current = asMap[key]
	}
	value, _ := current.(bool)
	return value
}

func mergeFakeConfigPatch(data map[string]any, patch map[string]any) map[string]any {
	if data == nil {
		data = map[string]any{}
	}
	for key, value := range patch {
		if childPatch, ok := value.(map[string]any); ok {
			childData, _ := data[key].(map[string]any)
			data[key] = mergeFakeConfigPatch(childData, childPatch)
			continue
		}
		data[key] = value
	}
	return data
}
