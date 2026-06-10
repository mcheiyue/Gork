package account

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jiujiu532/grok2api/app/platform"
)

type fakeRefreshRepo struct {
	mu       sync.Mutex
	records  []AccountRecord
	snapshot RuntimeSnapshot
	patches  []AccountPatch
	getCalls [][]string
}

func (r *fakeRefreshRepo) GetAccounts(_ context.Context, tokens []string) ([]AccountRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.getCalls = append(r.getCalls, append([]string(nil), tokens...))
	return append([]AccountRecord(nil), r.records...), nil
}

func (r *fakeRefreshRepo) PatchAccounts(_ context.Context, patches []AccountPatch) (AccountMutationResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.patches = append(r.patches, patches...)
	return AccountMutationResult{Patched: len(patches)}, nil
}

func (r *fakeRefreshRepo) RuntimeSnapshot(context.Context) (RuntimeSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.snapshot, nil
}

type fakeUsageFetcher struct {
	mu        sync.Mutex
	responses map[string]map[string]any
	err       error
	calls     []string
}

func (f *fakeUsageFetcher) FetchUsage(_ context.Context, token string, modeName string) (map[string]any, error) {
	f.mu.Lock()
	f.calls = append(f.calls, token+":"+modeName)
	err := f.err
	var response map[string]any
	if f.responses != nil {
		response = f.responses[modeName]
	}
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if response != nil {
		return response, nil
	}
	return map[string]any{"remainingQueries": 9, "totalQueries": 9, "windowSizeSeconds": 60}, nil
}

type blockingUsageFetcher struct {
	mu       sync.Mutex
	active   int
	max      int
	entered  chan struct{}
	release  <-chan struct{}
	response map[string]any
}

func (f *blockingUsageFetcher) FetchUsage(ctx context.Context, _ string, _ string) (map[string]any, error) {
	f.mu.Lock()
	f.active++
	if f.active > f.max {
		f.max = f.active
	}
	f.mu.Unlock()
	select {
	case f.entered <- struct{}{}:
	default:
	}
	defer func() {
		f.mu.Lock()
		f.active--
		f.mu.Unlock()
	}()
	select {
	case <-f.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if f.response != nil {
		return f.response, nil
	}
	return map[string]any{"remainingQueries": 9, "totalQueries": 9, "windowSizeSeconds": 60}, nil
}

func (f *blockingUsageFetcher) maxActive() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.max
}

func waitForFetchEntry(t *testing.T, entries <-chan struct{}) {
	t.Helper()
	select {
	case <-entries:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for fetch entry")
	}
}

func TestRefreshResultMerge(t *testing.T) {
	result := RefreshResult{Checked: 1, Refreshed: 2, Recovered: 3, Expired: 4, Disabled: 5, RateLimited: 6, Failed: 7}
	result.Merge(RefreshResult{Checked: 10, Refreshed: 20, Recovered: 30, Expired: 40, Disabled: 50, RateLimited: 60, Failed: 70})

	want := RefreshResult{Checked: 11, Refreshed: 22, Recovered: 33, Expired: 44, Disabled: 55, RateLimited: 66, Failed: 77}
	if result != want {
		t.Fatalf("Merge result = %#v, want %#v", result, want)
	}
}

func TestRefreshOnImportAppliesFetchedQuotasAndKeepsPythonCheckedCount(t *testing.T) {
	oldNow := refreshNowMS
	refreshNowMS = func() int64 { return 1000 }
	t.Cleanup(func() { refreshNowMS = oldNow })
	active := AccountRecord{Token: "tok-active", Pool: "basic", Status: AccountStatusActive, Quota: DefaultQuotaSet("basic").ToDict()}
	expired := AccountRecord{Token: "tok-expired", Pool: "basic", Status: AccountStatusExpired, Quota: DefaultQuotaSet("basic").ToDict()}
	repo := &fakeRefreshRepo{records: []AccountRecord{active, expired}}
	fetcher := &fakeUsageFetcher{}
	service := NewAccountRefreshService(repo, AccountRefreshOptions{Fetcher: fetcher, UsageConcurrency: 1})

	result, err := service.RefreshOnImport(context.Background(), []string{"tok-active", "tok-expired"})

	if err != nil {
		t.Fatalf("RefreshOnImport returned error: %v", err)
	}
	if result.Checked != 3 || result.Refreshed != 1 || result.Failed != 0 {
		t.Fatalf("RefreshOnImport result = %#v", result)
	}
	if len(repo.patches) != 1 {
		t.Fatalf("patch count = %d, want 1", len(repo.patches))
	}
	patch := repo.patches[0]
	if patch.UsageSyncDelta == nil || *patch.UsageSyncDelta != 1 || patch.LastSyncAt == nil {
		t.Fatalf("sync patch fields = %#v", patch)
	}
	if patch.QuotaFast["total"] != 30 || patch.QuotaFast["window_seconds"] != 86400 {
		t.Fatalf("basic fast quota should be normalized, got %#v", patch.QuotaFast)
	}
}

func TestRefreshOnImportInfersPoolFromEntitlementWindows(t *testing.T) {
	oldNow := refreshNowMS
	refreshNowMS = func() int64 { return 1500 }
	t.Cleanup(func() { refreshNowMS = oldNow })
	record := AccountRecord{Token: "tok-heavy", Pool: "basic", Status: AccountStatusActive, Quota: DefaultQuotaSet("basic").ToDict()}
	repo := &fakeRefreshRepo{records: []AccountRecord{record}}
	fetcher := &fakeUsageFetcher{responses: map[string]map[string]any{
		"auto":   {"remainingQueries": 0, "totalQueries": 0, "windowSizeSeconds": 0},
		"expert": {"remainingQueries": 150, "totalQueries": 150, "windowSizeSeconds": 7200},
	}}
	service := NewAccountRefreshService(repo, AccountRefreshOptions{Fetcher: fetcher, UsageConcurrency: 1})

	result, err := service.RefreshOnImport(context.Background(), []string{"tok-heavy"})

	if err != nil {
		t.Fatalf("RefreshOnImport returned error: %v", err)
	}
	if result.Checked != 2 || result.Refreshed != 1 || result.Failed != 0 {
		t.Fatalf("RefreshOnImport result = %#v", result)
	}
	if len(repo.patches) != 1 {
		t.Fatalf("patch count = %d, want 1", len(repo.patches))
	}
	patch := repo.patches[0]
	if patch.Pool == nil || *patch.Pool != "heavy" {
		t.Fatalf("pool patch = %#v, want heavy", patch.Pool)
	}
	if patch.QuotaExpert["total"] != 150 || patch.QuotaExpert["remaining"] != 150 {
		t.Fatalf("expert quota should be kept after heavy inference, got %#v", patch.QuotaExpert)
	}
}

func TestRefreshCallAsyncConsoleDecrementsLocalQuota(t *testing.T) {
	oldNow := refreshNowMS
	refreshNowMS = func() int64 { return 2000 }
	t.Cleanup(func() { refreshNowMS = oldNow })
	quota := DefaultQuotaSet("basic")
	quota.Console.Remaining = 20
	quota.Console.ResetAt = nil
	record := AccountRecord{Token: "tok-console", Pool: "basic", Status: AccountStatusActive, Quota: quota.ToDict()}
	repo := &fakeRefreshRepo{records: []AccountRecord{record}}
	fetcher := &fakeUsageFetcher{}
	service := NewAccountRefreshService(repo, AccountRefreshOptions{Fetcher: fetcher})

	if err := service.RefreshCallAsync(context.Background(), "tok-console", 5); err != nil {
		t.Fatalf("RefreshCallAsync returned error: %v", err)
	}

	if len(fetcher.calls) != 0 {
		t.Fatalf("console mode should not call usage API, calls = %#v", fetcher.calls)
	}
	patch := repo.patches[0]
	if patch.UsageUseDelta == nil || *patch.UsageUseDelta != 1 || patch.LastUseAt == nil || *patch.LastUseAt != 2000 {
		t.Fatalf("use patch fields = %#v", patch)
	}
	if patch.QuotaConsole["remaining"] != 19 || patch.QuotaConsole["reset_at"] != nil || patch.QuotaConsole["source"] != int(QuotaSourceEstimated) {
		t.Fatalf("console quota patch = %#v", patch.QuotaConsole)
	}
}

func TestRefreshCallAsyncConsoleStartsResetTimerAtThreshold(t *testing.T) {
	oldNow := refreshNowMS
	refreshNowMS = func() int64 { return 3000 }
	t.Cleanup(func() { refreshNowMS = oldNow })
	quota := DefaultQuotaSet("basic")
	quota.Console.Remaining = 16
	quota.Console.ResetAt = nil
	record := AccountRecord{Token: "tok-console-threshold", Pool: "basic", Status: AccountStatusActive, Quota: quota.ToDict()}
	repo := &fakeRefreshRepo{records: []AccountRecord{record}}
	service := NewAccountRefreshService(repo, AccountRefreshOptions{Fetcher: &fakeUsageFetcher{}})

	if err := service.RefreshCallAsync(context.Background(), "tok-console-threshold", 5); err != nil {
		t.Fatalf("RefreshCallAsync returned error: %v", err)
	}

	patch := repo.patches[0]
	if patch.QuotaConsole["remaining"] != 15 || patch.QuotaConsole["reset_at"] != int64(903000) {
		t.Fatalf("console threshold quota patch = %#v", patch.QuotaConsole)
	}
}

func TestResetExpiredConsoleWindowsRestoresDefaultQuota(t *testing.T) {
	oldNow := refreshNowMS
	refreshNowMS = func() int64 { return 1_000_000 }
	t.Cleanup(func() { refreshNowMS = oldNow })
	expiredAt := int64(999_000)
	activeAt := int64(1_001_000)
	needsResetQuota := DefaultQuotaSet("basic")
	needsResetQuota.Console.Remaining = 7
	needsResetQuota.Console.ResetAt = &expiredAt
	fullQuota := DefaultQuotaSet("basic")
	fullQuota.Console.Remaining = fullQuota.Console.Total
	fullQuota.Console.ResetAt = &expiredAt
	activeQuota := DefaultQuotaSet("basic")
	activeQuota.Console.Remaining = 5
	activeQuota.Console.ResetAt = &activeAt
	repo := &fakeRefreshRepo{snapshot: RuntimeSnapshot{Items: []AccountRecord{
		{Token: "tok-reset", Pool: "basic", Status: AccountStatusActive, Quota: needsResetQuota.ToDict()},
		{Token: "tok-full", Pool: "basic", Status: AccountStatusActive, Quota: fullQuota.ToDict()},
		{Token: "tok-active", Pool: "basic", Status: AccountStatusActive, Quota: activeQuota.ToDict()},
	}}}
	service := NewAccountRefreshService(repo, AccountRefreshOptions{Fetcher: &fakeUsageFetcher{}})

	count, err := service.ResetExpiredConsoleWindows(context.Background())

	if err != nil {
		t.Fatalf("ResetExpiredConsoleWindows returned error: %v", err)
	}
	if count != 1 {
		t.Fatalf("reset count = %d, want 1", count)
	}
	if len(repo.patches) != 1 || repo.patches[0].Token != "tok-reset" {
		t.Fatalf("reset patches = %#v", repo.patches)
	}
	patch := repo.patches[0].QuotaConsole
	if patch["remaining"] != 30 || patch["total"] != 30 || patch["reset_at"] != nil || patch["source"] != int(QuotaSourceDefault) {
		t.Fatalf("reset console quota patch = %#v", patch)
	}
}

func TestRefreshScheduledFiltersPoolAndManageable(t *testing.T) {
	oldNow := refreshNowMS
	refreshNowMS = func() int64 { return 5000 }
	t.Cleanup(func() { refreshNowMS = oldNow })
	basic := AccountRecord{Token: "tok-basic", Pool: "basic", Status: AccountStatusActive, Quota: DefaultQuotaSet("basic").ToDict()}
	super := AccountRecord{Token: "tok-super", Pool: "super", Status: AccountStatusActive, Quota: DefaultQuotaSet("super").ToDict()}
	expired := AccountRecord{Token: "tok-expired", Pool: "basic", Status: AccountStatusExpired, Quota: DefaultQuotaSet("basic").ToDict()}
	repo := &fakeRefreshRepo{snapshot: RuntimeSnapshot{Items: []AccountRecord{basic, super, expired}}}
	fetcher := &fakeUsageFetcher{}
	service := NewAccountRefreshService(repo, AccountRefreshOptions{Fetcher: fetcher, UsageConcurrency: 1})
	pool := "basic"

	result, err := service.RefreshScheduled(context.Background(), &pool)

	if err != nil {
		t.Fatalf("RefreshScheduled returned error: %v", err)
	}
	if result.Checked != 1 || result.Refreshed != 1 || result.Failed != 0 {
		t.Fatalf("RefreshScheduled result = %#v", result)
	}
	if len(repo.patches) != 1 || repo.patches[0].Token != "tok-basic" {
		t.Fatalf("scheduled patches = %#v", repo.patches)
	}
	prefix := "tok-basic:"
	for _, call := range fetcher.calls {
		if len(call) < len(prefix) || call[:len(prefix)] != prefix {
			t.Fatalf("unexpected fetch call after pool filter: %s", call)
		}
	}
}

func TestRefreshOnDemandThrottlesAfterSuccessfulRefresh(t *testing.T) {
	oldNow := refreshNowMS
	refreshNowMS = func() int64 { return 6000 }
	t.Cleanup(func() { refreshNowMS = oldNow })
	record := AccountRecord{Token: "tok-demand", Pool: "basic", Status: AccountStatusActive, Quota: DefaultQuotaSet("basic").ToDict()}
	repo := &fakeRefreshRepo{snapshot: RuntimeSnapshot{Items: []AccountRecord{record}}}
	service := NewAccountRefreshService(repo, AccountRefreshOptions{
		Fetcher:             &fakeUsageFetcher{},
		UsageConcurrency:    1,
		OnDemandMinInterval: time.Hour,
	})

	first, err := service.RefreshOnDemand(context.Background())
	if err != nil {
		t.Fatalf("first RefreshOnDemand returned error: %v", err)
	}
	second, err := service.RefreshOnDemand(context.Background())
	if err != nil {
		t.Fatalf("second RefreshOnDemand returned error: %v", err)
	}
	if first.Checked != 1 || first.Refreshed != 1 || first.Failed != 0 {
		t.Fatalf("first on-demand result = %#v", first)
	}
	if second != (RefreshResult{}) {
		t.Fatalf("second on-demand should be throttled, got %#v", second)
	}
	if len(repo.patches) != 1 {
		t.Fatalf("on-demand patch count = %d, want 1", len(repo.patches))
	}
}

func TestRefreshTokensFiltersManageableAndAggregates(t *testing.T) {
	oldNow := refreshNowMS
	refreshNowMS = func() int64 { return 7000 }
	t.Cleanup(func() { refreshNowMS = oldNow })
	active := AccountRecord{Token: "tok-token", Pool: "basic", Status: AccountStatusActive, Quota: DefaultQuotaSet("basic").ToDict()}
	expired := AccountRecord{Token: "tok-expired", Pool: "basic", Status: AccountStatusExpired, Quota: DefaultQuotaSet("basic").ToDict()}
	repo := &fakeRefreshRepo{records: []AccountRecord{active, expired}}
	service := NewAccountRefreshService(repo, AccountRefreshOptions{Fetcher: &fakeUsageFetcher{}, UsageConcurrency: 1})

	result, err := service.RefreshTokens(context.Background(), []string{"tok-token", "tok-expired"})

	if err != nil {
		t.Fatalf("RefreshTokens returned error: %v", err)
	}
	if result.Checked != 1 || result.Refreshed != 1 || result.Failed != 0 {
		t.Fatalf("RefreshTokens result = %#v", result)
	}
	if len(repo.patches) != 1 || repo.patches[0].Token != "tok-token" {
		t.Fatalf("RefreshTokens patches = %#v", repo.patches)
	}
}

func TestRefreshOnImportChunksLargeTokenLookup(t *testing.T) {
	oldLimit := refreshAccountLookupBatchSize
	refreshAccountLookupBatchSize = 2
	t.Cleanup(func() { refreshAccountLookupBatchSize = oldLimit })
	repo := &fakeRefreshRepo{}
	service := NewAccountRefreshService(repo, AccountRefreshOptions{Fetcher: &fakeUsageFetcher{}, UsageConcurrency: 1})

	result, err := service.RefreshOnImport(context.Background(), []string{"tok-1", "tok-2", "tok-3", "tok-4", "tok-5"})

	if err != nil {
		t.Fatalf("RefreshOnImport returned error: %v", err)
	}
	if result.Checked != 0 {
		t.Fatalf("RefreshOnImport result = %#v", result)
	}
	if len(repo.getCalls) != 3 || len(repo.getCalls[0]) != 2 || len(repo.getCalls[1]) != 2 || len(repo.getCalls[2]) != 1 {
		t.Fatalf("GetAccounts calls = %#v", repo.getCalls)
	}
}

func TestRefreshTokensChunksLargeTokenLookup(t *testing.T) {
	oldLimit := refreshAccountLookupBatchSize
	refreshAccountLookupBatchSize = 2
	t.Cleanup(func() { refreshAccountLookupBatchSize = oldLimit })
	repo := &fakeRefreshRepo{}
	service := NewAccountRefreshService(repo, AccountRefreshOptions{Fetcher: &fakeUsageFetcher{}, UsageConcurrency: 1})

	result, err := service.RefreshTokens(context.Background(), []string{"tok-1", "tok-2", "tok-3", "tok-4", "tok-5"})

	if err != nil {
		t.Fatalf("RefreshTokens returned error: %v", err)
	}
	if result != (RefreshResult{}) {
		t.Fatalf("RefreshTokens result = %#v", result)
	}
	if len(repo.getCalls) != 3 || len(repo.getCalls[0]) != 2 || len(repo.getCalls[1]) != 2 || len(repo.getCalls[2]) != 1 {
		t.Fatalf("GetAccounts calls = %#v", repo.getCalls)
	}
}

func TestSetQuotaPatchMapsAllModeFields(t *testing.T) {
	patch := AccountPatch{Token: "tok"}

	for modeID := 0; modeID <= 5; modeID++ {
		setQuotaPatch(&patch, modeID, map[string]any{"mode": modeID})
	}

	if patch.QuotaAuto["mode"] != 0 || patch.QuotaFast["mode"] != 1 || patch.QuotaExpert["mode"] != 2 ||
		patch.QuotaHeavy["mode"] != 3 || patch.QuotaGrok43["mode"] != 4 || patch.QuotaConsole["mode"] != 5 {
		t.Fatalf("quota field mapping = %#v", patch)
	}
}

func TestRunRefreshBatchHonorsUsageConcurrency(t *testing.T) {
	release := make(chan struct{})
	releaseOnce := sync.Once{}
	defer releaseOnce.Do(func() { close(release) })
	fetcher := &blockingUsageFetcher{
		entered: make(chan struct{}, 4),
		release: release,
	}
	service := NewAccountRefreshService(&fakeRefreshRepo{}, AccountRefreshOptions{Fetcher: fetcher, UsageConcurrency: 2})
	records := []AccountRecord{
		{Token: "tok-a", Pool: "basic", Status: AccountStatusActive, Quota: DefaultQuotaSet("basic").ToDict()},
		{Token: "tok-b", Pool: "basic", Status: AccountStatusActive, Quota: DefaultQuotaSet("basic").ToDict()},
	}
	done := make(chan error, 1)
	go func() {
		_, err := service.runRefreshBatch(context.Background(), records, false, false)
		done <- err
	}()

	waitForFetchEntry(t, fetcher.entered)
	waitForFetchEntry(t, fetcher.entered)
	releaseOnce.Do(func() { close(release) })
	if err := <-done; err != nil {
		t.Fatalf("runRefreshBatch returned error: %v", err)
	}
	if fetcher.maxActive() < 2 {
		t.Fatalf("max active fetches = %d, want at least 2", fetcher.maxActive())
	}
}

func TestRefreshCallAsyncInvalidCredentialsExpiresAccount(t *testing.T) {
	oldNow := invalidCredentialsNowMS
	invalidCredentialsNowMS = func() int64 { return 8000 }
	t.Cleanup(func() { invalidCredentialsNowMS = oldNow })
	record := AccountRecord{Token: "tok-invalid", Pool: "basic", Status: AccountStatusActive, Quota: DefaultQuotaSet("basic").ToDict()}
	repo := &fakeRefreshRepo{records: []AccountRecord{record}}
	service := NewAccountRefreshService(repo, AccountRefreshOptions{
		Fetcher: &fakeUsageFetcher{err: platform.NewUpstreamError("bad", 403, "token expired")},
	})

	if err := service.RefreshCallAsync(context.Background(), "tok-invalid", 1); err != nil {
		t.Fatalf("RefreshCallAsync returned error: %v", err)
	}
	if len(repo.patches) != 1 {
		t.Fatalf("invalid credential patch count = %d, want 1", len(repo.patches))
	}
	patch := repo.patches[0]
	if patch.Status == nil || *patch.Status != AccountStatusExpired {
		t.Fatalf("invalid credential status patch = %#v", patch.Status)
	}
	if patch.LastFailAt == nil || *patch.LastFailAt != 8000 {
		t.Fatalf("invalid credential last fail at = %#v", patch.LastFailAt)
	}
	if patch.LastFailReason == nil || *patch.LastFailReason != "invalid_credentials" {
		t.Fatalf("invalid credential reason = %#v", patch.LastFailReason)
	}
	if patch.StateReason == nil || *patch.StateReason != "invalid_credentials" {
		t.Fatalf("invalid credential state reason = %#v", patch.StateReason)
	}
}

func TestRefreshOneAppliesFallbackForFetchFailure(t *testing.T) {
	oldNow := refreshNowMS
	refreshNowMS = func() int64 { return 10000 }
	t.Cleanup(func() { refreshNowMS = oldNow })
	quota := DefaultQuotaSet("super")
	quota.Fast.Source = QuotaSourceReal
	quota.Fast.Remaining = 4
	record := AccountRecord{Token: "tok-fallback", Pool: "super", Status: AccountStatusActive, Quota: quota.ToDict()}
	repo := &fakeRefreshRepo{}
	service := NewAccountRefreshService(repo, AccountRefreshOptions{Fetcher: &fakeUsageFetcher{err: errors.New("network")}})

	result, err := service.refreshOne(context.Background(), record, true, false)

	if err != nil {
		t.Fatalf("refreshOne returned error: %v", err)
	}
	if result.Checked != 1 || result.Failed != 1 {
		t.Fatalf("fallback result = %#v", result)
	}
	if len(repo.patches) != 1 || repo.patches[0].QuotaFast["remaining"] != 3 || repo.patches[0].QuotaFast["source"] != int(QuotaSourceEstimated) {
		t.Fatalf("fallback patch = %#v", repo.patches)
	}
}

func TestRecordFailureAsyncRateLimitedPatchesEstimatedQuota(t *testing.T) {
	oldNow := refreshNowMS
	refreshNowMS = func() int64 { return 3000 }
	t.Cleanup(func() { refreshNowMS = oldNow })
	resetAt := int64(9000)
	quota := DefaultQuotaSet("basic")
	quota.Fast.Remaining = 5
	quota.Fast.ResetAt = &resetAt
	record := AccountRecord{Token: "tok-rate", Pool: "basic", Status: AccountStatusActive, Quota: quota.ToDict()}
	repo := &fakeRefreshRepo{records: []AccountRecord{record}}
	service := NewAccountRefreshService(repo, AccountRefreshOptions{})

	err := service.RecordFailureAsync(context.Background(), "tok-rate", 1, platform.NewUpstreamError("rate", 429, ""))

	if err != nil {
		t.Fatalf("RecordFailureAsync returned error: %v", err)
	}
	patch := repo.patches[0]
	if patch.UsageFailDelta == nil || *patch.UsageFailDelta != 1 || patch.LastFailAt == nil || *patch.LastFailAt != 3000 {
		t.Fatalf("failure patch fields = %#v", patch)
	}
	if patch.LastFailReason == nil || *patch.LastFailReason != "rate_limited" {
		t.Fatalf("last fail reason = %#v", patch.LastFailReason)
	}
	if patch.QuotaFast["remaining"] != 0 || patch.QuotaFast["reset_at"] != resetAt || patch.QuotaFast["source"] != int(QuotaSourceEstimated) {
		t.Fatalf("rate-limited quota patch = %#v", patch.QuotaFast)
	}
}
