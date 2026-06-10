package app

import (
	"context"
	"fmt"
	"math"
	"strconv"

	accountcontrol "github.com/jiujiu532/grok2api/app/control/account"
	accountbackends "github.com/jiujiu532/grok2api/app/control/account/backends"
	configbackends "github.com/jiujiu532/grok2api/app/platform/config/backends"
	platformstartup "github.com/jiujiu532/grok2api/app/platform/startup"
)

var (
	appMainCreateConfigBackend = configbackends.CreateConfigBackend
	appMainStartupMigrator     = platformstartup.RunStartupMigrations
)

func runAppMainStartupMigrations(ctx context.Context, state *appMainLifecycleState) (Hook, error) {
	if state == nil || state.repository == nil {
		return nil, nil
	}
	env := appMainEnv()
	configBackend, err := appMainCreateConfigBackend(configbackends.FactoryOptions{Env: env})
	if err != nil {
		return nil, err
	}
	defer configBackend.Close(ctx)
	repositoryBackend, err := accountbackends.GetRepositoryBackend(env)
	if err != nil {
		return nil, err
	}
	err = appMainStartupMigrator(ctx, configBackend, appMainStartupRepository{repo: state.repository}, platformstartup.StartupMigrationOptions{
		ConfigBackendName:     configbackends.GetConfigBackendName(env),
		RepositoryBackendName: repositoryBackend,
	})
	return nil, err
}

type appMainStartupRepository struct {
	repo accountcontrol.AccountRepository
}

func (r appMainStartupRepository) RuntimeSnapshot(ctx context.Context) (platformstartup.AccountRuntimeSnapshot, error) {
	snapshot, err := r.repo.RuntimeSnapshot(ctx)
	if err != nil {
		return platformstartup.AccountRuntimeSnapshot{}, err
	}
	return platformstartup.AccountRuntimeSnapshot{
		Revision: int64(snapshot.Revision),
		Items:    appMainStartupRecords(snapshot.Items),
	}, nil
}

func (r appMainStartupRepository) ListAccounts(ctx context.Context, query platformstartup.ListAccountsQuery) (platformstartup.ListAccountsResult, error) {
	page, err := r.repo.ListAccounts(ctx, accountcontrol.ListAccountsQuery{
		Page:           query.Page,
		PageSize:       query.PageSize,
		Pool:           appMainStringPtr(query.Pool),
		IncludeDeleted: query.IncludeDeleted,
	})
	if err != nil {
		return platformstartup.ListAccountsResult{}, err
	}
	return platformstartup.ListAccountsResult{
		Items:      appMainStartupRecords(page.Items),
		TotalPages: page.TotalPages,
	}, nil
}

func (r appMainStartupRepository) UpsertAccounts(ctx context.Context, items []platformstartup.AccountUpsert) (platformstartup.AccountPatchResult, error) {
	result, err := r.repo.UpsertAccounts(ctx, appMainControlUpserts(items))
	return platformstartup.AccountPatchResult{Patched: result.Upserted}, err
}

func (r appMainStartupRepository) PatchAccounts(ctx context.Context, patches []platformstartup.AccountPatch) (platformstartup.AccountPatchResult, error) {
	result, err := r.repo.PatchAccounts(ctx, appMainControlPatches(patches))
	return platformstartup.AccountPatchResult{Patched: result.Patched}, err
}

func (r appMainStartupRepository) DeleteAccounts(ctx context.Context, tokens []string) (platformstartup.AccountPatchResult, error) {
	result, err := r.repo.DeleteAccounts(ctx, tokens)
	return platformstartup.AccountPatchResult{Patched: result.Deleted}, err
}

func appMainStartupRecords(records []accountcontrol.AccountRecord) []platformstartup.AccountRecord {
	out := make([]platformstartup.AccountRecord, 0, len(records))
	for _, record := range records {
		out = append(out, platformstartup.AccountRecord{
			Token:          record.Token,
			Pool:           record.Pool,
			Status:         string(record.Status),
			Tags:           append([]string(nil), record.Tags...),
			Ext:            appMainCloneMap(record.Ext),
			Quotas:         appMainStartupQuotas(record.Quota),
			UsageUseCount:  record.UsageUseCount,
			UsageFailCount: record.UsageFailCount,
			UsageSyncCount: record.UsageSyncCount,
			LastUseAt:      appMainAnyFromInt64Ptr(record.LastUseAt),
			LastFailAt:     appMainAnyFromInt64Ptr(record.LastFailAt),
			LastFailReason: appMainStringFromPtr(record.LastFailReason),
			LastSyncAt:     appMainAnyFromInt64Ptr(record.LastSyncAt),
			LastClearAt:    appMainAnyFromInt64Ptr(record.LastClearAt),
			StateReason:    appMainStringFromPtr(record.StateReason),
			DeletedAt:      appMainAnyFromInt64Ptr(record.DeletedAt),
		})
	}
	return out
}

func appMainStartupQuotas(quota map[string]any) platformstartup.AccountQuotaSet {
	return platformstartup.AccountQuotaSet{
		Auto:    appMainMapValue(quota, "auto"),
		Fast:    appMainMapValue(quota, "fast"),
		Expert:  appMainMapValue(quota, "expert"),
		Heavy:   appMainMapValue(quota, "heavy"),
		Grok43:  appMainMapValue(quota, "grok_4_3"),
		Console: appMainMapValue(quota, "console"),
	}
}

func appMainControlUpserts(items []platformstartup.AccountUpsert) []accountcontrol.AccountUpsert {
	out := make([]accountcontrol.AccountUpsert, 0, len(items))
	for _, item := range items {
		out = append(out, accountcontrol.AccountUpsert{
			Token: item.Token,
			Pool:  item.Pool,
			Tags:  append([]string(nil), item.Tags...),
			Ext:   appMainCloneMap(item.Ext),
		})
	}
	return out
}

func appMainControlPatches(patches []platformstartup.AccountPatch) []accountcontrol.AccountPatch {
	out := make([]accountcontrol.AccountPatch, 0, len(patches))
	for _, patch := range patches {
		out = append(out, accountcontrol.AccountPatch{
			Token:          patch.Token,
			Status:         appMainStatusPtr(patch.Status),
			QuotaAuto:      appMainCloneMap(patch.QuotaAuto),
			QuotaFast:      appMainCloneMap(patch.QuotaFast),
			QuotaExpert:    appMainCloneMap(patch.QuotaExpert),
			QuotaHeavy:     appMainCloneMap(patch.QuotaHeavy),
			QuotaGrok43:    appMainCloneMap(patch.QuotaGrok43),
			QuotaConsole:   appMainCloneMap(patch.QuotaConsole),
			UsageUseDelta:  patch.UsageUseDelta,
			UsageFailDelta: patch.UsageFailDelta,
			UsageSyncDelta: patch.UsageSyncDelta,
			LastUseAt:      appMainInt64PtrFromAny(patch.LastUseAt),
			LastFailAt:     appMainInt64PtrFromAny(patch.LastFailAt),
			LastFailReason: appMainStringPtr(patch.LastFailReason),
			LastSyncAt:     appMainInt64PtrFromAny(patch.LastSyncAt),
			LastClearAt:    appMainInt64PtrFromAny(patch.LastClearAt),
			StateReason:    appMainStringPtr(patch.StateReason),
			ExtMerge:       appMainCloneMap(patch.ExtMerge),
		})
	}
	return out
}

func appMainMapValue(data map[string]any, key string) map[string]any {
	value, ok := data[key]
	if !ok {
		return nil
	}
	typed, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return appMainCloneMap(typed)
}

func appMainCloneMap(data map[string]any) map[string]any {
	if len(data) == 0 {
		return nil
	}
	out := make(map[string]any, len(data))
	for key, value := range data {
		out[key] = value
	}
	return out
}

func appMainStringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func appMainStatusPtr(value string) *accountcontrol.AccountStatus {
	if value == "" {
		return nil
	}
	status := accountcontrol.AccountStatus(value)
	return &status
}

func appMainStringFromPtr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func appMainAnyFromInt64Ptr(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func appMainInt64PtrFromAny(value any) *int64 {
	switch typed := value.(type) {
	case nil:
		return nil
	case int64:
		return &typed
	case int:
		converted := int64(typed)
		return &converted
	case int32:
		converted := int64(typed)
		return &converted
	case float64:
		if math.Trunc(typed) != typed {
			return nil
		}
		converted := int64(typed)
		return &converted
	case string:
		parsed, err := strconv.ParseInt(typed, 10, 64)
		if err != nil {
			return nil
		}
		return &parsed
	default:
		parsed, err := strconv.ParseInt(fmt.Sprint(typed), 10, 64)
		if err != nil {
			return nil
		}
		return &parsed
	}
}
