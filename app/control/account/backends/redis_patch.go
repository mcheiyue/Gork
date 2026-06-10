package backends

import account "github.com/jiujiu532/grok2api/app/control/account"

func appendRedisBasicUpdates(updates map[string]string, patch account.AccountPatch) {
	if patch.Status != nil {
		updates["status"] = patch.Status.String()
	}
	if patch.StateReason != nil {
		updates["state_reason"] = *patch.StateReason
	}
	if patch.LastUseAt != nil {
		updates["last_use_at"] = formatInt64(*patch.LastUseAt)
	}
	if patch.LastFailAt != nil {
		updates["last_fail_at"] = formatInt64(*patch.LastFailAt)
	}
	if patch.LastFailReason != nil {
		updates["last_fail_reason"] = *patch.LastFailReason
	}
	if patch.LastSyncAt != nil {
		updates["last_sync_at"] = formatInt64(*patch.LastSyncAt)
	}
	if patch.LastClearAt != nil {
		updates["last_clear_at"] = formatInt64(*patch.LastClearAt)
	}
	if patch.Pool != nil {
		updates["pool"] = *patch.Pool
	}
}

func appendRedisUsageUpdates(
	updates map[string]string,
	record account.AccountRecord,
	patch account.AccountPatch,
) {
	if patch.UsageUseDelta != nil {
		updates["usage_use_count"] = formatInt(maxInt(0, record.UsageUseCount+*patch.UsageUseDelta))
	}
	if patch.UsageFailDelta != nil {
		updates["usage_fail_count"] = formatInt(maxInt(0, record.UsageFailCount+*patch.UsageFailDelta))
	}
	if patch.UsageSyncDelta != nil {
		updates["usage_sync_count"] = formatInt(maxInt(0, record.UsageSyncCount+*patch.UsageSyncDelta))
	}
}

func appendRedisQuotaUpdates(updates map[string]string, patch account.AccountPatch) error {
	for _, item := range []struct {
		field string
		value map[string]any
	}{
		{"quota_auto", patch.QuotaAuto},
		{"quota_fast", patch.QuotaFast},
		{"quota_expert", patch.QuotaExpert},
		{"quota_heavy", patch.QuotaHeavy},
		{"quota_grok_4_3", patch.QuotaGrok43},
		{"quota_console", patch.QuotaConsole},
	} {
		if item.value == nil {
			continue
		}
		raw, err := jsonString(item.value)
		if err != nil {
			return err
		}
		updates[item.field] = raw
	}
	return nil
}

func appendRedisExtUpdates(
	updates map[string]string,
	record account.AccountRecord,
	patch account.AccountPatch,
) (map[string]string, error) {
	ext := cloneAnyMap(record.Ext)
	for key, value := range patch.ExtMerge {
		ext[key] = value
	}
	if patch.ClearFailures {
		for _, key := range failureExtKeys {
			delete(ext, key)
		}
		updates["status"] = account.AccountStatusActive.String()
		updates["usage_fail_count"] = "0"
		updates["last_fail_at"] = ""
		updates["last_fail_reason"] = ""
		updates["state_reason"] = ""
	}
	raw, err := jsonString(ext)
	if err != nil {
		return nil, err
	}
	updates["ext"] = raw
	return updates, nil
}

func patchedRedisTags(current []string, patch account.AccountPatch) string {
	tags := append([]string{}, current...)
	if patch.Tags != nil {
		tags = append([]string{}, patch.Tags...)
	}
	for _, tag := range patch.AddTags {
		if !hasString(tags, tag) {
			tags = append(tags, tag)
		}
	}
	if len(patch.RemoveTags) > 0 {
		filtered := []string{}
		for _, tag := range tags {
			if !hasString(patch.RemoveTags, tag) {
				filtered = append(filtered, tag)
			}
		}
		tags = filtered
	}
	raw, _ := jsonString(tags)
	return raw
}

func formatInt(value int) string {
	return formatInt64(int64(value))
}
