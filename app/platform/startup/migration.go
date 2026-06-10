package startup

import (
	"context"
	"fmt"
	"os"
	"strings"

	platformpaths "github.com/jiujiu532/grok2api/app/platform"
	platformconfig "github.com/jiujiu532/grok2api/app/platform/config"
)

const defaultMigrationBatch = 500

type ConfigBackend interface {
	Load(context.Context) (map[string]any, error)
	ApplyPatch(context.Context, map[string]any) error
	Version(context.Context) (any, error)
}

type AccountRepository interface {
	RuntimeSnapshot(context.Context) (AccountRuntimeSnapshot, error)
	ListAccounts(context.Context, ListAccountsQuery) (ListAccountsResult, error)
	UpsertAccounts(context.Context, []AccountUpsert) (AccountPatchResult, error)
	PatchAccounts(context.Context, []AccountPatch) (AccountPatchResult, error)
	DeleteAccounts(context.Context, []string) (AccountPatchResult, error)
}

type LocalAccountSource interface {
	ListAccounts(context.Context, ListAccountsQuery) (ListAccountsResult, error)
	Close(context.Context) error
}

type StartupMigrationOptions struct {
	ConfigBackendName     string
	RepositoryBackendName string
	DefaultsPath          string
	UserConfigPath        string
	LocalDBPath           string
	BatchSize             int
	LoadTOML              func(string) (map[string]any, error)
	CopyFile              func(string, string) error
	Rename                func(string, string) error
	LocalSourceFactory    func(string) (LocalAccountSource, error)
	DefaultQuotaWindow    func(pool string, kind int) map[string]any
	NormalizeQuotaSet     func(pool string, quotas AccountQuotaSet) AccountQuotaSet
}

type AccountRuntimeSnapshot struct {
	Revision int64
	Items    []AccountRecord
}

type ListAccountsQuery struct {
	Page           int
	PageSize       int
	Pool           string
	IncludeDeleted bool
}

type ListAccountsResult struct {
	Items      []AccountRecord
	TotalPages int
}

type AccountPatchResult struct {
	Patched int
}

type AccountQuotaSet struct {
	Auto    map[string]any
	Fast    map[string]any
	Expert  map[string]any
	Heavy   map[string]any
	Grok43  map[string]any
	Console map[string]any
}

type AccountRecord struct {
	Token          string
	Pool           string
	Status         string
	Tags           []string
	Ext            map[string]any
	Quotas         AccountQuotaSet
	UsageUseCount  int
	UsageFailCount int
	UsageSyncCount int
	LastUseAt      any
	LastFailAt     any
	LastFailReason string
	LastSyncAt     any
	LastClearAt    any
	StateReason    string
	DeletedAt      any
}

type AccountUpsert struct {
	Token string
	Pool  string
	Tags  []string
	Ext   map[string]any
}

type AccountPatch struct {
	Token          string
	Status         string
	QuotaAuto      map[string]any
	QuotaFast      map[string]any
	QuotaExpert    map[string]any
	QuotaHeavy     map[string]any
	QuotaGrok43    map[string]any
	QuotaConsole   map[string]any
	UsageUseDelta  *int
	UsageFailDelta *int
	UsageSyncDelta *int
	LastUseAt      any
	LastFailAt     any
	LastFailReason string
	LastSyncAt     any
	LastClearAt    any
	StateReason    string
	ExtMerge       map[string]any
}

func RunStartupMigrations(ctx context.Context, config ConfigBackend, repo AccountRepository, options StartupMigrationOptions) error {
	options = normalizeOptions(options)
	if err := migrateConfig(ctx, config, options); err != nil {
		return err
	}
	if err := migrateBasicRefreshInterval(ctx, config); err != nil {
		return err
	}
	if err := migrateAccounts(ctx, repo, options); err != nil {
		return err
	}
	if err := backfillGrok43Quota(ctx, config, repo, options); err != nil {
		return err
	}
	if err := normalizeBasicFastOnlyQuota(ctx, repo, options); err != nil {
		return err
	}
	return backfillConsoleQuota(ctx, repo, options)
}

func migrateConfig(ctx context.Context, backend ConfigBackend, options StartupMigrationOptions) error {
	if options.ConfigBackendName == "local" {
		if !fileExists(options.UserConfigPath) && fileExists(options.DefaultsPath) {
			return options.CopyFile(options.DefaultsPath, options.UserConfigPath)
		}
		return nil
	}
	version, err := backend.Version(ctx)
	if err != nil || !versionIsZero(version) {
		return err
	}
	if !fileExists(options.UserConfigPath) {
		return nil
	}
	userData, err := options.LoadTOML(options.UserConfigPath)
	if err != nil || len(userData) == 0 {
		return err
	}
	return backend.ApplyPatch(ctx, userData)
}

func migrateBasicRefreshInterval(ctx context.Context, backend ConfigBackend) error {
	data, err := backend.Load(ctx)
	if err != nil {
		return err
	}
	refresh := nestedMap(nestedMap(data, "account"), "refresh")
	value, ok := toInt(refresh["basic_interval_sec"])
	if !ok || value != 36000 {
		return nil
	}
	return backend.ApplyPatch(ctx, map[string]any{"account": map[string]any{"refresh": map[string]any{"basic_interval_sec": 86400}}})
}

func migrateAccounts(ctx context.Context, target AccountRepository, options StartupMigrationOptions) error {
	if options.RepositoryBackendName == "local" || !fileExists(options.LocalDBPath) {
		return nil
	}
	snapshot, err := target.RuntimeSnapshot(ctx)
	if err != nil || snapshot.Revision > 0 || len(snapshot.Items) > 0 {
		return err
	}
	if _, err := copyAccounts(ctx, options.LocalDBPath, target, options); err != nil {
		return err
	}
	return options.Rename(options.LocalDBPath, migratedDBPath(options.LocalDBPath))
}

func copyAccounts(ctx context.Context, sqlitePath string, target AccountRepository, options StartupMigrationOptions) (int, error) {
	if options.LocalSourceFactory == nil {
		return 0, fmt.Errorf("local account source factory is required")
	}
	source, err := options.LocalSourceFactory(sqlitePath)
	if err != nil {
		return 0, err
	}
	defer source.Close(ctx)
	total := 0
	for page := 1; ; page++ {
		result, err := source.ListAccounts(ctx, ListAccountsQuery{Page: page, PageSize: options.BatchSize, IncludeDeleted: true})
		if err != nil || len(result.Items) == 0 {
			return total, err
		}
		if _, err := target.UpsertAccounts(ctx, accountUpserts(result.Items)); err != nil {
			return total, err
		}
		if _, err := target.PatchAccounts(ctx, accountPatches(result.Items)); err != nil {
			return total, err
		}
		if deleted := deletedTokens(result.Items); len(deleted) > 0 {
			if _, err := target.DeleteAccounts(ctx, deleted); err != nil {
				return total, err
			}
		}
		total += len(result.Items)
		if page >= result.TotalPages {
			return total, nil
		}
	}
}

func accountUpserts(records []AccountRecord) []AccountUpsert {
	out := make([]AccountUpsert, 0, len(records))
	for _, record := range records {
		out = append(out, AccountUpsert{Token: record.Token, Pool: record.Pool, Tags: record.Tags, Ext: record.Ext})
	}
	return out
}

func accountPatches(records []AccountRecord) []AccountPatch {
	out := make([]AccountPatch, 0, len(records))
	for _, record := range records {
		out = append(out, recordToPatch(record))
	}
	return out
}

func recordToPatch(record AccountRecord) AccountPatch {
	return AccountPatch{
		Token: record.Token, Status: record.Status,
		QuotaAuto: record.Quotas.Auto, QuotaFast: record.Quotas.Fast, QuotaExpert: record.Quotas.Expert,
		QuotaHeavy: record.Quotas.Heavy, QuotaGrok43: record.Quotas.Grok43, QuotaConsole: record.Quotas.Console,
		UsageUseDelta: intPtrIfNonzero(record.UsageUseCount), UsageFailDelta: intPtrIfNonzero(record.UsageFailCount),
		UsageSyncDelta: intPtrIfNonzero(record.UsageSyncCount), LastUseAt: record.LastUseAt,
		LastFailAt: record.LastFailAt, LastFailReason: record.LastFailReason, LastSyncAt: record.LastSyncAt,
		LastClearAt: record.LastClearAt, StateReason: record.StateReason, ExtMerge: nilIfEmptyMap(record.Ext),
	}
}

func normalizeOptions(options StartupMigrationOptions) StartupMigrationOptions {
	if options.ConfigBackendName == "" {
		options.ConfigBackendName = strings.TrimSpace(os.Getenv("ACCOUNT_STORAGE"))
	}
	if options.ConfigBackendName == "" {
		options.ConfigBackendName = "local"
	}
	if options.RepositoryBackendName == "" {
		options.RepositoryBackendName = strings.TrimSpace(os.Getenv("ACCOUNT_STORAGE"))
	}
	if options.RepositoryBackendName == "" {
		options.RepositoryBackendName = "local"
	}
	if options.BatchSize <= 0 {
		options.BatchSize = defaultMigrationBatch
	}
	if options.UserConfigPath == "" {
		options.UserConfigPath = platformpaths.DataPath("config.toml")
	}
	if options.DefaultsPath == "" {
		options.DefaultsPath = "config.defaults.toml"
	}
	if options.LocalDBPath == "" {
		options.LocalDBPath = platformpaths.DataPath("accounts.db")
	}
	if options.LoadTOML == nil {
		options.LoadTOML = platformconfig.LoadTOML
	}
	if options.CopyFile == nil {
		options.CopyFile = copyFile
	}
	if options.Rename == nil {
		options.Rename = os.Rename
	}
	return options
}

func countKeys(nested map[string]any) int {
	count := 0
	for _, value := range nested {
		if child, ok := value.(map[string]any); ok {
			count += countKeys(child)
			continue
		}
		count++
	}
	return count
}
