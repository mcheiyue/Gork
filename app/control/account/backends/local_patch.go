package backends

import (
	"context"
	"database/sql"
	"sort"
	"strings"

	account "github.com/jiujiu532/grok2api/app/control/account"
	platformruntime "github.com/jiujiu532/grok2api/app/platform/runtime"
)

func patchLocalAccounts(
	ctx context.Context,
	tx localSQLRunner,
	patches []account.AccountPatch,
	revision int,
) (int, error) {
	ts := platformruntime.NowMS()
	count := 0
	for _, patch := range patches {
		affected, err := patchLocalAccount(ctx, tx, patch, ts, revision)
		if err != nil {
			return 0, err
		}
		count += affected
	}
	return count, nil
}

func patchLocalAccount(
	ctx context.Context,
	tx localSQLRunner,
	patch account.AccountPatch,
	ts int64,
	revision int,
) (int, error) {
	record, found, err := getLocalAccountForPatch(ctx, tx, patch.Token)
	if err != nil || !found {
		return 0, err
	}
	sets, err := buildLocalPatchSets(record, patch, ts, revision)
	if err != nil {
		return 0, err
	}
	assignments, values := localPatchAssignments(sets)
	values = append(values, patch.Token)
	result, err := tx.ExecContext(
		ctx,
		"UPDATE "+localAccountTable+" SET "+assignments+" WHERE token = ?",
		values...,
	)
	if err != nil {
		return 0, err
	}
	return affectedRows(result)
}

func getLocalAccountForPatch(
	ctx context.Context,
	tx localSQLRunner,
	token string,
) (account.AccountRecord, bool, error) {
	row := tx.QueryRowContext(
		ctx,
		"SELECT "+localAccountColumns+" FROM "+localAccountTable+" WHERE token = ?",
		token,
	)
	record, err := scanLocalAccount(row)
	if err == sql.ErrNoRows {
		return account.AccountRecord{}, false, nil
	}
	return record, err == nil, err
}

func buildLocalPatchSets(
	record account.AccountRecord,
	patch account.AccountPatch,
	ts int64,
	revision int,
) ([]localPatchSet, error) {
	sets := []localPatchSet{{"updated_at", ts}, {"revision", revision}}
	sets = appendBasicPatchSets(sets, patch)
	sets = appendUsagePatchSets(sets, record, patch)
	quotaSets, err := quotaPatchSets(patch)
	if err != nil {
		return nil, err
	}
	sets = append(sets, quotaSets...)
	sets = append(sets, localPatchSet{"tags", patchedTags(record.Tags, patch)})
	return appendExtPatchSets(sets, record, patch)
}

func appendBasicPatchSets(sets []localPatchSet, patch account.AccountPatch) []localPatchSet {
	if patch.Pool != nil {
		sets = append(sets, localPatchSet{"pool", *patch.Pool})
	}
	if patch.Status != nil {
		sets = append(sets, localPatchSet{"status", patch.Status.String()})
	}
	for _, item := range timestampPatchSets(patch) {
		sets = append(sets, item)
	}
	return sets
}

func timestampPatchSets(patch account.AccountPatch) []localPatchSet {
	sets := []localPatchSet{}
	if patch.StateReason != nil {
		sets = append(sets, localPatchSet{"state_reason", *patch.StateReason})
	}
	if patch.LastUseAt != nil {
		sets = append(sets, localPatchSet{"last_use_at", *patch.LastUseAt})
	}
	if patch.LastFailAt != nil {
		sets = append(sets, localPatchSet{"last_fail_at", *patch.LastFailAt})
	}
	if patch.LastFailReason != nil {
		sets = append(sets, localPatchSet{"last_fail_reason", *patch.LastFailReason})
	}
	if patch.LastSyncAt != nil {
		sets = append(sets, localPatchSet{"last_sync_at", *patch.LastSyncAt})
	}
	if patch.LastClearAt != nil {
		sets = append(sets, localPatchSet{"last_clear_at", *patch.LastClearAt})
	}
	return sets
}

func appendUsagePatchSets(
	sets []localPatchSet,
	record account.AccountRecord,
	patch account.AccountPatch,
) []localPatchSet {
	if patch.UsageUseDelta != nil {
		sets = append(sets, localPatchSet{"usage_use_count", maxInt(0, record.UsageUseCount+*patch.UsageUseDelta)})
	}
	if patch.UsageFailDelta != nil {
		sets = append(sets, localPatchSet{"usage_fail_count", maxInt(0, record.UsageFailCount+*patch.UsageFailDelta)})
	}
	if patch.UsageSyncDelta != nil {
		sets = append(sets, localPatchSet{"usage_sync_count", maxInt(0, record.UsageSyncCount+*patch.UsageSyncDelta)})
	}
	return sets
}

func quotaPatchSets(patch account.AccountPatch) ([]localPatchSet, error) {
	items := []struct {
		column string
		value  map[string]any
	}{
		{"quota_auto", patch.QuotaAuto},
		{"quota_fast", patch.QuotaFast},
		{"quota_expert", patch.QuotaExpert},
		{"quota_heavy", patch.QuotaHeavy},
		{"quota_grok_4_3", patch.QuotaGrok43},
		{"quota_console", patch.QuotaConsole},
	}
	sets := []localPatchSet{}
	for _, item := range items {
		if item.value == nil {
			continue
		}
		raw, err := jsonString(item.value)
		if err != nil {
			return nil, err
		}
		sets = append(sets, localPatchSet{item.column, raw})
	}
	return sets, nil
}

func appendExtPatchSets(
	sets []localPatchSet,
	record account.AccountRecord,
	patch account.AccountPatch,
) ([]localPatchSet, error) {
	ext := cloneAnyMap(record.Ext)
	for key, value := range patch.ExtMerge {
		ext[key] = value
	}
	if patch.ClearFailures {
		for _, key := range failureExtKeys {
			delete(ext, key)
		}
		sets = append(sets,
			localPatchSet{"status", account.AccountStatusActive.String()},
			localPatchSet{"usage_fail_count", 0},
			localPatchSet{"last_fail_at", nil},
			localPatchSet{"last_fail_reason", nil},
			localPatchSet{"state_reason", nil},
		)
	}
	raw, err := jsonString(ext)
	if err != nil {
		return nil, err
	}
	return append(sets, localPatchSet{"ext", raw}), nil
}

func patchedTags(current []string, patch account.AccountPatch) string {
	tagSet := map[string]struct{}{}
	for _, tag := range current {
		tagSet[tag] = struct{}{}
	}
	if patch.Tags != nil {
		tagSet = map[string]struct{}{}
		for _, tag := range patch.Tags {
			tagSet[tag] = struct{}{}
		}
	}
	for _, tag := range patch.AddTags {
		tagSet[tag] = struct{}{}
	}
	for _, tag := range patch.RemoveTags {
		delete(tagSet, tag)
	}
	tags := make([]string, 0, len(tagSet))
	for tag := range tagSet {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	raw, _ := jsonString(tags)
	return raw
}

func localPatchAssignments(sets []localPatchSet) (string, []any) {
	assignments := make([]string, 0, len(sets))
	values := make([]any, 0, len(sets))
	for _, set := range sets {
		assignments = append(assignments, set.column+" = ?")
		values = append(values, set.value)
	}
	return strings.Join(assignments, ", "), values
}

func cloneAnyMap(src map[string]any) map[string]any {
	dst := map[string]any{}
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

var failureExtKeys = []string{
	"cooldown_until",
	"cooldown_reason",
	"disabled_at",
	"disabled_reason",
	"expired_at",
	"expired_reason",
	"forbidden_strikes",
}
