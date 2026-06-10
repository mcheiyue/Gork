package account

import (
	"encoding/json"
	"reflect"
	"testing"
)

func int64Ptr(value int64) *int64 {
	return &value
}

func TestQuotaWindowRoundTripAndState(t *testing.T) {
	window, err := QuotaWindowFromDict(map[string]any{
		"remaining":      "0",
		"total":          10.0,
		"window_seconds": "60",
		"reset_at":       1000,
		"synced_at":      "900",
		"source":         int(QuotaSourceReal),
	})
	if err != nil {
		t.Fatalf("QuotaWindowFromDict returned error: %v", err)
	}

	if !window.IsExhausted() {
		t.Fatal("quota window with zero remaining should be exhausted")
	}
	if !window.IsWindowExpired(1000) {
		t.Fatal("quota window should be expired at reset timestamp")
	}
	got := window.ToDict()
	if got["remaining"] != 0 || got["total"] != 10 || got["window_seconds"] != 60 {
		t.Fatalf("quota numeric dict = %#v", got)
	}
	if got["reset_at"] != int64(1000) || got["synced_at"] != int64(900) {
		t.Fatalf("quota timestamp dict = %#v", got)
	}
	if got["source"] != int(QuotaSourceReal) {
		t.Fatalf("quota source = %#v, want %d", got["source"], QuotaSourceReal)
	}
}

func TestAccountQuotaSetModeMappingAndSerialization(t *testing.T) {
	auto := QuotaWindow{Remaining: 1, Total: 10, WindowSeconds: 60, Source: QuotaSourceDefault}
	fast := QuotaWindow{Remaining: 2, Total: 20, WindowSeconds: 60, Source: QuotaSourceReal}
	expert := QuotaWindow{Remaining: 3, Total: 30, WindowSeconds: 60, Source: QuotaSourceEstimated}
	heavy := QuotaWindow{Remaining: 4, Total: 40, WindowSeconds: 60, Source: QuotaSourceReal}
	grok := QuotaWindow{Remaining: 5, Total: 50, WindowSeconds: 60, Source: QuotaSourceReal}
	console := QuotaWindow{Remaining: 6, Total: 60, WindowSeconds: 60, Source: QuotaSourceReal}
	quotaSet := AccountQuotaSet{Auto: auto, Fast: fast, Expert: expert, Heavy: &heavy, Grok43: &grok}

	if got := quotaSet.Get(0); got == nil || got.Total != 10 {
		t.Fatalf("Get(0) = %#v, want auto quota", got)
	}
	if got := quotaSet.Get(4); got == nil || got.Total != 50 {
		t.Fatalf("Get(4) = %#v, want grok_4_3 quota", got)
	}
	if got := quotaSet.Get(5); got != nil {
		t.Fatalf("Get(5) = %#v, want nil before console is set", got)
	}
	quotaSet.Set(99, console)
	if got := quotaSet.Get(5); got == nil || got.Total != 60 {
		t.Fatalf("Set(unknown) should set console, got %#v", got)
	}

	serialized := quotaSet.ToDict()
	for _, key := range []string{"auto", "fast", "expert", "heavy", "grok_4_3", "console"} {
		if _, ok := serialized[key]; !ok {
			t.Fatalf("serialized quota set missing %q: %#v", key, serialized)
		}
	}
	restored, err := AccountQuotaSetFromDict(serialized)
	if err != nil {
		t.Fatalf("AccountQuotaSetFromDict returned error: %v", err)
	}
	if got := restored.Get(3); got == nil || got.Total != 40 {
		t.Fatalf("restored heavy quota = %#v, want total 40", got)
	}

	withoutOptional, err := AccountQuotaSetFromDict(map[string]any{
		"auto":       auto.ToDict(),
		"fast":       fast.ToDict(),
		"expert":     expert.ToDict(),
		"heavy":      map[string]any{},
		"grok_4_3":   map[string]any{},
		"console":    nil,
		"extra_mode": map[string]any{"remaining": 99},
	})
	if err != nil {
		t.Fatalf("AccountQuotaSetFromDict with empty optional windows returned error: %v", err)
	}
	if withoutOptional.Heavy != nil || withoutOptional.Grok43 != nil || withoutOptional.Console != nil {
		t.Fatalf("empty optional quota windows = %#v, want nil optional windows", withoutOptional)
	}
}

func TestAccountUsageStatsRoundTrip(t *testing.T) {
	stats, err := AccountUsageStatsFromDict(map[string]any{
		"use_count":  "3",
		"fail_count": 2.0,
		"sync_count": 1,
	})
	if err != nil {
		t.Fatalf("AccountUsageStatsFromDict returned error: %v", err)
	}
	if stats.UseCount != 3 || stats.FailCount != 2 || stats.SyncCount != 1 {
		t.Fatalf("stats = %#v", stats)
	}
	if got := stats.ToDict(); !reflect.DeepEqual(got, map[string]int{"use_count": 3, "fail_count": 2, "sync_count": 1}) {
		t.Fatalf("stats dict = %#v", got)
	}
}

func TestAccountRecordNormalizersMatchPythonModel(t *testing.T) {
	token, err := NormalizeAccountToken(" sso=abc\u2010def\u200b\n ")
	if err != nil {
		t.Fatalf("NormalizeAccountToken returned error: %v", err)
	}
	if token != "abc-def" {
		t.Fatalf("token = %q, want abc-def", token)
	}
	if _, err := NormalizeAccountToken(" \u200b "); err == nil {
		t.Fatal("empty normalized token should fail")
	}
	for input, want := range map[any]string{
		"AUTO":     "basic",
		"ssobasic": "basic",
		"super":    "super",
		"heavy":    "heavy",
		nil:        "super",
		"":         "super",
		"s":        "super",
	} {
		got, err := NormalizeAccountPool(input)
		if err != nil {
			t.Fatalf("NormalizeAccountPool(%#v) returned error: %v", input, err)
		}
		if got != want {
			t.Fatalf("NormalizeAccountPool(%#v) = %q, want %q", input, got, want)
		}
	}
	if _, err := NormalizeAccountPool("enterprise"); err == nil {
		t.Fatal("unknown pool should fail")
	}
	gotTags := NormalizeAccountTags(" nsfw, api ,nsfw,, ")
	if !reflect.DeepEqual(gotTags, []string{"nsfw", "api"}) {
		t.Fatalf("tags = %#v, want nsfw/api", gotTags)
	}
}

func TestNewAccountRecordDefaultsAndQuotaCopy(t *testing.T) {
	oldNow := accountRecordNowMS
	accountRecordNowMS = func() int64 { return 1234 }
	t.Cleanup(func() { accountRecordNowMS = oldNow })
	quotaSet := AccountQuotaSet{
		Auto:   QuotaWindow{Remaining: 1, Total: 1, Source: QuotaSourceDefault},
		Fast:   QuotaWindow{Remaining: 2, Total: 2, Source: QuotaSourceDefault},
		Expert: QuotaWindow{Remaining: 3, Total: 3, Source: QuotaSourceDefault},
	}

	record, err := NewAccountRecord(AccountRecord{
		Token: " sso=tok ",
		Pool:  "super",
		Tags:  []string{"nsfw", "nsfw", "beta"},
	})
	if err != nil {
		t.Fatalf("NewAccountRecord returned error: %v", err)
	}
	if record.Token != "tok" || record.Status != AccountStatusActive || record.CreatedAt != 1234 || record.UpdatedAt != 1234 {
		t.Fatalf("record defaults = %#v", record)
	}
	if !record.IsNSFW() || !record.IsSuper() || record.IsHeavy() || record.IsDeleted() {
		t.Fatalf("record state helpers mismatch: %#v", record)
	}
	withQuota := record.WithQuotaSet(quotaSet)
	if len(record.Quota) != 0 {
		t.Fatalf("WithQuotaSet mutated original quota: %#v", record.Quota)
	}
	restored, err := withQuota.QuotaSet()
	if err != nil {
		t.Fatalf("QuotaSet returned error: %v", err)
	}
	if restored.Auto.Total != 1 || restored.Fast.Total != 2 || restored.Expert.Total != 3 {
		t.Fatalf("restored quota set = %#v", restored)
	}
}

func TestAccountRecordJSONShapeIncludesPythonFields(t *testing.T) {
	lastUseAt := int64(2001)
	lastFailAt := int64(2002)
	lastSyncAt := int64(2003)
	lastClearAt := int64(2004)
	deletedAt := int64(2005)
	lastFailReason := "rate"
	stateReason := "manual"
	record := AccountRecord{
		Token:          "tok",
		Pool:           "heavy",
		Status:         AccountStatusExpired,
		CreatedAt:      1001,
		UpdatedAt:      1002,
		Tags:           []string{"nsfw", "api"},
		Quota:          map[string]any{"auto": map[string]any{"remaining": 1}},
		UsageUseCount:  3,
		UsageFailCount: 4,
		UsageSyncCount: 5,
		LastUseAt:      &lastUseAt,
		LastFailAt:     &lastFailAt,
		LastFailReason: &lastFailReason,
		LastSyncAt:     &lastSyncAt,
		LastClearAt:    &lastClearAt,
		StateReason:    &stateReason,
		DeletedAt:      &deletedAt,
		Ext:            map[string]any{"source": "test"},
		Revision:       7,
	}

	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal AccountRecord: %v", err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatalf("unmarshal AccountRecord keys: %v", err)
	}
	for _, key := range []string{
		"token", "pool", "status", "created_at", "updated_at", "tags", "quota",
		"usage_use_count", "usage_fail_count", "usage_sync_count",
		"last_use_at", "last_fail_at", "last_fail_reason", "last_sync_at", "last_clear_at",
		"state_reason", "deleted_at", "ext", "revision",
	} {
		if _, ok := keys[key]; !ok {
			t.Fatalf("AccountRecord JSON missing %q: %s", key, raw)
		}
	}

	var decoded AccountRecord
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal AccountRecord: %v", err)
	}
	if decoded.Token != record.Token || decoded.Pool != record.Pool || decoded.Status != record.Status {
		t.Fatalf("decoded basic fields = %#v", decoded)
	}
	if decoded.UsageUseCount != 3 || decoded.UsageFailCount != 4 || decoded.UsageSyncCount != 5 {
		t.Fatalf("decoded usage fields = %#v", decoded)
	}
	if decoded.LastUseAt == nil || *decoded.LastUseAt != lastUseAt ||
		decoded.LastFailAt == nil || *decoded.LastFailAt != lastFailAt ||
		decoded.LastSyncAt == nil || *decoded.LastSyncAt != lastSyncAt ||
		decoded.LastClearAt == nil || *decoded.LastClearAt != lastClearAt ||
		decoded.DeletedAt == nil || *decoded.DeletedAt != deletedAt {
		t.Fatalf("decoded timestamp fields = %#v", decoded)
	}
	if decoded.LastFailReason == nil || *decoded.LastFailReason != lastFailReason ||
		decoded.StateReason == nil || *decoded.StateReason != stateReason {
		t.Fatalf("decoded reason fields = %#v", decoded)
	}
	if !decoded.IsNSFW() || !decoded.IsHeavy() || !decoded.IsDeleted() || decoded.IsSuper() {
		t.Fatalf("decoded helper state mismatch = %#v", decoded)
	}
}

func TestLightweightResultDefaults(t *testing.T) {
	mutation := AccountMutationResult{}
	if mutation.Upserted != 0 || mutation.Patched != 0 || mutation.Deleted != 0 || mutation.Revision != 0 {
		t.Fatalf("mutation result defaults = %#v", mutation)
	}
	mutationRaw, err := json.Marshal(mutation)
	if err != nil {
		t.Fatalf("marshal mutation result: %v", err)
	}
	if string(mutationRaw) != `{"upserted":0,"patched":0,"deleted":0,"revision":0}` {
		t.Fatalf("mutation result json = %s", mutationRaw)
	}

	page := NewAccountPage()
	if page.Page != 1 || page.PageSize != 50 || page.TotalPages != 1 || len(page.Items) != 0 {
		t.Fatalf("account page defaults = %#v", page)
	}
	pageRaw, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("marshal account page: %v", err)
	}
	if string(pageRaw) != `{"items":[],"total":0,"page":1,"page_size":50,"total_pages":1,"revision":0}` {
		t.Fatalf("account page json = %s", pageRaw)
	}
	changes := NewAccountChangeSet()
	if len(changes.Items) != 0 || len(changes.DeletedTokens) != 0 || changes.HasMore {
		t.Fatalf("change set defaults = %#v", changes)
	}
	changesRaw, err := json.Marshal(changes)
	if err != nil {
		t.Fatalf("marshal change set: %v", err)
	}
	if string(changesRaw) != `{"revision":0,"items":[],"deleted_tokens":[],"has_more":false}` {
		t.Fatalf("change set json = %s", changesRaw)
	}
	snapshot := NewRuntimeSnapshot()
	if len(snapshot.Items) != 0 || snapshot.Revision != 0 {
		t.Fatalf("runtime snapshot defaults = %#v", snapshot)
	}
	snapshotRaw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal runtime snapshot: %v", err)
	}
	if string(snapshotRaw) != `{"revision":0,"items":[]}` {
		t.Fatalf("runtime snapshot json = %s", snapshotRaw)
	}
}
