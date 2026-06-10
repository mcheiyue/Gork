package backends

import (
	"strconv"

	account "github.com/jiujiu532/grok2api/app/control/account"
	platformruntime "github.com/jiujiu532/grok2api/app/platform/runtime"
)

func redisHashFromRecord(record account.AccountRecord, revision int) (map[string]string, error) {
	quota, err := record.QuotaSet()
	if err != nil {
		return nil, err
	}
	tags, err := jsonString(record.Tags)
	if err != nil {
		return nil, err
	}
	ext, err := jsonString(record.Ext)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"pool":             record.Pool,
		"status":           record.Status.String(),
		"created_at":       formatInt64(record.CreatedAt),
		"updated_at":       formatInt64(record.UpdatedAt),
		"tags":             tags,
		"quota_auto":       mustQuotaJSON(quota.Auto),
		"quota_fast":       mustQuotaJSON(quota.Fast),
		"quota_expert":     mustQuotaJSON(quota.Expert),
		"quota_heavy":      optionalQuotaJSON(quota.Heavy),
		"quota_grok_4_3":   optionalQuotaJSON(quota.Grok43),
		"quota_console":    optionalQuotaJSON(quota.Console),
		"usage_use_count":  strconv.Itoa(record.UsageUseCount),
		"usage_fail_count": strconv.Itoa(record.UsageFailCount),
		"usage_sync_count": strconv.Itoa(record.UsageSyncCount),
		"last_use_at":      formatOptionalInt64(record.LastUseAt),
		"last_fail_at":     formatOptionalInt64(record.LastFailAt),
		"last_fail_reason": formatOptionalString(record.LastFailReason),
		"last_sync_at":     formatOptionalInt64(record.LastSyncAt),
		"last_clear_at":    formatOptionalInt64(record.LastClearAt),
		"state_reason":     formatOptionalString(record.StateReason),
		"deleted_at":       formatOptionalInt64(record.DeletedAt),
		"ext":              ext,
		"revision":         strconv.Itoa(revision),
	}, nil
}

func redisRecordFromHash(token string, hash map[string]string) (account.AccountRecord, error) {
	now := platformruntime.NowMS()
	tags, err := stringSliceFromJSON(hashValue(hash, "tags", "[]"))
	if err != nil {
		return account.AccountRecord{}, err
	}
	ext, err := anyMapFromJSON(hashValue(hash, "ext", "{}"))
	if err != nil {
		return account.AccountRecord{}, err
	}
	quota, err := redisQuotaFromHash(hash)
	if err != nil {
		return account.AccountRecord{}, err
	}
	return account.NewAccountRecord(account.AccountRecord{
		Token:          token,
		Pool:           hashValue(hash, "pool", "basic"),
		Status:         account.AccountStatus(hashValue(hash, "status", "active")),
		CreatedAt:      int64Value(hash["created_at"], now),
		UpdatedAt:      int64Value(hash["updated_at"], now),
		Tags:           tags,
		Quota:          quota,
		UsageUseCount:  intValue(hash["usage_use_count"], 0),
		UsageFailCount: intValue(hash["usage_fail_count"], 0),
		UsageSyncCount: intValue(hash["usage_sync_count"], 0),
		LastUseAt:      optionalInt64String(hash["last_use_at"]),
		LastFailAt:     optionalInt64String(hash["last_fail_at"]),
		LastFailReason: optionalString(hash["last_fail_reason"]),
		LastSyncAt:     optionalInt64String(hash["last_sync_at"]),
		LastClearAt:    optionalInt64String(hash["last_clear_at"]),
		StateReason:    optionalString(hash["state_reason"]),
		DeletedAt:      optionalInt64String(hash["deleted_at"]),
		Ext:            ext,
		Revision:       intValue(hash["revision"], 0),
	})
}

func redisQuotaFromHash(hash map[string]string) (map[string]any, error) {
	quota := map[string]any{}
	for _, item := range []struct {
		name     string
		field    string
		required bool
	}{
		{"auto", "quota_auto", true},
		{"fast", "quota_fast", true},
		{"expert", "quota_expert", true},
		{"heavy", "quota_heavy", false},
		{"grok_4_3", "quota_grok_4_3", false},
		{"console", "quota_console", false},
	} {
		data, err := anyMapFromJSON(hashValue(hash, item.field, "{}"))
		if err != nil {
			return nil, err
		}
		if item.required || len(data) > 0 {
			quota[item.name] = data
		}
	}
	return quota, nil
}

func hashValue(hash map[string]string, key string, fallback string) string {
	if value := hash[key]; value != "" {
		return value
	}
	return fallback
}

func intValue(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func int64Value(raw string, fallback int64) int64 {
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fallback
	}
	return value
}

func optionalInt64String(raw string) *int64 {
	if raw == "" || raw == "None" {
		return nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil
	}
	return &value
}

func optionalString(raw string) *string {
	if raw == "" {
		return nil
	}
	return &raw
}

func formatInt64(value int64) string {
	return strconv.FormatInt(value, 10)
}

func formatOptionalInt64(value *int64) string {
	if value == nil {
		return ""
	}
	return formatInt64(*value)
}

func formatOptionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
