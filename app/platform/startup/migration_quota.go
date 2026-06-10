package startup

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

const grok43QuotaBackfillMarker = "grok43_quota_backfill"

func backfillGrok43Quota(ctx context.Context, config ConfigBackend, repo AccountRepository, options StartupMigrationOptions) error {
	if options.DefaultQuotaWindow == nil {
		return nil
	}
	if done, err := startupMigrationMarked(ctx, config, grok43QuotaBackfillMarker); err != nil || done {
		return err
	}
	patches := []AccountPatch{}
	for _, pool := range []string{"super", "heavy"} {
		for page := 1; ; page++ {
			result, err := repo.ListAccounts(ctx, ListAccountsQuery{Page: page, PageSize: options.BatchSize, Pool: pool, IncludeDeleted: false})
			if err != nil {
				return err
			}
			for _, record := range result.Items {
				if record.Quotas.Grok43 != nil {
					continue
				}
				if window := options.DefaultQuotaWindow(record.Pool, 4); window != nil {
					patches = append(patches, AccountPatch{Token: record.Token, QuotaGrok43: window})
				}
			}
			if page >= result.TotalPages {
				break
			}
		}
	}
	if err := applyMigrationPatches(ctx, repo, patches, options.BatchSize); err != nil {
		return err
	}
	return markStartupMigration(ctx, config, grok43QuotaBackfillMarker)
}

func normalizeBasicFastOnlyQuota(ctx context.Context, repo AccountRepository, options StartupMigrationOptions) error {
	if options.NormalizeQuotaSet == nil {
		return nil
	}
	patches := []AccountPatch{}
	for page := 1; ; page++ {
		result, err := repo.ListAccounts(ctx, ListAccountsQuery{Page: page, PageSize: options.BatchSize, Pool: "basic", IncludeDeleted: false})
		if err != nil {
			return err
		}
		for _, record := range result.Items {
			normalized := options.NormalizeQuotaSet("basic", record.Quotas)
			if reflect.DeepEqual(normalized, record.Quotas) {
				continue
			}
			patches = append(patches, AccountPatch{
				Token: record.Token, QuotaAuto: normalized.Auto, QuotaFast: normalized.Fast, QuotaExpert: normalized.Expert,
			})
		}
		if page >= result.TotalPages {
			break
		}
	}
	return applyMigrationPatches(ctx, repo, patches, options.BatchSize)
}

func backfillConsoleQuota(ctx context.Context, repo AccountRepository, options StartupMigrationOptions) error {
	if options.DefaultQuotaWindow == nil {
		return nil
	}
	patches := []AccountPatch{}
	for page := 1; ; page++ {
		result, err := repo.ListAccounts(ctx, ListAccountsQuery{Page: page, PageSize: options.BatchSize, IncludeDeleted: false})
		if err != nil {
			return err
		}
		for _, record := range result.Items {
			if record.Quotas.Console != nil {
				continue
			}
			if window := options.DefaultQuotaWindow(record.Pool, 5); window != nil {
				patches = append(patches, AccountPatch{Token: record.Token, QuotaConsole: window})
			}
		}
		if page >= result.TotalPages {
			break
		}
	}
	return applyMigrationPatches(ctx, repo, patches, options.BatchSize)
}

func applyMigrationPatches(ctx context.Context, repo AccountRepository, patches []AccountPatch, batchSize int) error {
	if len(patches) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = defaultMigrationBatch
	}
	for start := 0; start < len(patches); start += batchSize {
		end := start + batchSize
		if end > len(patches) {
			end = len(patches)
		}
		if _, err := repo.PatchAccounts(ctx, patches[start:end]); err != nil {
			return err
		}
	}
	return nil
}

func startupMigrationMarked(ctx context.Context, config ConfigBackend, name string) (bool, error) {
	data, err := config.Load(ctx)
	if err != nil {
		return false, err
	}
	startup, ok := data["startup"].(map[string]any)
	if !ok {
		return false, nil
	}
	migrations, ok := startup["migrations"].(map[string]any)
	if !ok {
		return false, nil
	}
	marked, _ := migrations[name].(bool)
	return marked, nil
}

func markStartupMigration(ctx context.Context, config ConfigBackend, name string) error {
	return config.ApplyPatch(ctx, map[string]any{
		"startup": map[string]any{
			"migrations": map[string]any{name: true},
		},
	})
}

func versionIsZero(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case int:
		return typed == 0
	case int64:
		return typed == 0
	case float64:
		return typed == 0
	case string:
		return strings.TrimSpace(typed) == "" || strings.TrimSpace(typed) == "0"
	default:
		return false
	}
}

func toInt(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		return parsed, err == nil
	default:
		return 0, false
	}
}

func nestedMap(root map[string]any, key string) map[string]any {
	if root == nil {
		return map[string]any{}
	}
	if nested, ok := root[key].(map[string]any); ok {
		return nested
	}
	return map[string]any{}
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := osStat(path)
	return err == nil
}

var osStat = func(path string) (bool, error) {
	_, err := filepath.Abs(path)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	return err == nil, err
}

func migratedDBPath(path string) string {
	ext := filepath.Ext(path)
	if ext == "" {
		return path + ".migrated"
	}
	return strings.TrimSuffix(path, ext) + ext + ".migrated"
}

func deletedTokens(records []AccountRecord) []string {
	tokens := []string{}
	for _, record := range records {
		if record.DeletedAt != nil {
			tokens = append(tokens, record.Token)
		}
	}
	return tokens
}

func intPtrIfNonzero(value int) *int {
	if value == 0 {
		return nil
	}
	return &value
}

func nilIfEmptyMap(value map[string]any) map[string]any {
	if len(value) == 0 {
		return nil
	}
	return value
}
