package account

import (
	"context"
	"testing"

	controlaccount "github.com/jiujiu532/grok2api/app/control/account"
	appruntime "github.com/jiujiu532/grok2api/app/platform/runtime"
)

func TestAccountDirectoryBootstrapSyncAndDiagnosticsMatchPython(t *testing.T) {
	ctx := context.Background()
	repo := &syncFakeRepository{snapshot: controlaccount.RuntimeSnapshot{
		Revision: 2,
		Items: []controlaccount.AccountRecord{
			accountRecord("first", "basic", basicQuotaSet(8), []string{"blue"}),
		},
	}}
	directory := NewAccountDirectory(repo)

	changed, err := directory.SyncIfChanged(ctx)
	if err != nil {
		t.Fatalf("SyncIfChanged before bootstrap returned error: %v", err)
	}
	if changed || len(repo.scanCalls) != 0 {
		t.Fatalf("SyncIfChanged before bootstrap changed=%t scanCalls=%#v", changed, repo.scanCalls)
	}
	if directory.Size() != 0 || directory.Revision() != 0 {
		t.Fatalf("initial diagnostics = size %d revision %d", directory.Size(), directory.Revision())
	}

	if err := directory.Bootstrap(ctx); err != nil {
		t.Fatalf("Bootstrap returned error: %v", err)
	}
	if directory.Size() != 1 || directory.Revision() != 2 {
		t.Fatalf("post-bootstrap diagnostics = size %d revision %d", directory.Size(), directory.Revision())
	}

	repo.changes = []controlaccount.AccountChangeSet{{
		Revision: 3,
		Items: []controlaccount.AccountRecord{
			accountRecord("second", "super", superQuotaSet(11), []string{"green"}),
		},
	}}
	changed, err = directory.SyncIfChanged(ctx)
	if err != nil {
		t.Fatalf("SyncIfChanged returned error: %v", err)
	}
	if !changed || directory.Size() != 2 || directory.Revision() != 3 {
		t.Fatalf("sync result changed=%t size=%d revision=%d", changed, directory.Size(), directory.Revision())
	}
	if len(repo.scanCalls) != 1 || repo.scanCalls[0] != 2 {
		t.Fatalf("ScanChanges revisions = %#v", repo.scanCalls)
	}
}

func TestAccountDirectoryReserveReleaseMatchesPython(t *testing.T) {
	mustSetDirectoryStrategy(t, "quota")
	ctx := context.Background()
	repo := &syncFakeRepository{snapshot: controlaccount.RuntimeSnapshot{
		Revision: 1,
		Items: []controlaccount.AccountRecord{
			accountRecord("basic-blue", "basic", basicQuotaSet(7), []string{"blue"}),
			accountRecord("basic-red", "basic", basicQuotaSet(9), []string{"red"}),
			accountRecord("super-gold", "super", superQuotaSet(13), []string{"gold"}),
		},
	}}
	directory := NewAccountDirectory(repo)
	if err := directory.Bootstrap(ctx); err != nil {
		t.Fatalf("Bootstrap returned error: %v", err)
	}

	now := 1234
	lease, ok := directory.Reserve(
		[]int{0, 1},
		1,
		ReserveOptions{
			ExcludeTokens: []string{"basic-blue"},
			PreferTags:    []string{"red"},
			NowS:          intPtr(now),
		},
	)
	if !ok {
		t.Fatalf("Reserve returned no lease")
	}
	if lease.Token != "basic-red" || lease.PoolID != 0 || lease.ModeID != 1 || lease.SelectedAt != now {
		t.Fatalf("lease = %#v", lease)
	}
	idx := directory.table.IdxByToken["basic-red"]
	if directory.table.InflightByIdx[idx] != 1 || directory.table.LastUseAtByIdx[idx] != now {
		t.Fatalf("reserve counters inflight=%d lastUse=%d", directory.table.InflightByIdx[idx], directory.table.LastUseAtByIdx[idx])
	}

	directory.Release(lease)
	if directory.table.InflightByIdx[idx] != 0 {
		t.Fatalf("release inflight = %d", directory.table.InflightByIdx[idx])
	}

	_, ok = directory.Reserve(
		0,
		1,
		ReserveOptions{
			ExcludeTokens: []string{"basic-blue", "basic-red"},
			NowS:          intPtr(now),
		},
	)
	if ok {
		t.Fatalf("Reserve with an int pool candidate selected an excluded account")
	}
}

func TestAccountDirectoryReserveAnyMatchesPythonNoModeQuota(t *testing.T) {
	mustSetDirectoryStrategy(t, "quota")
	table := MakeEmptyTable()
	idx := table.AppendSlot(AccountSlot{
		Token:      "ws-token",
		PoolID:     0,
		StatusID:   StatusActive,
		QuotaAuto:  1,
		WindowAuto: 60,
		QuotaFast:  0,
		Health:     0.8,
		Tags:       []string{"ws"},
	})
	directory := accountDirectoryWithTable(table)

	now := 2468
	lease, ok := directory.ReserveAny(
		[]int{0},
		ReserveOptions{PreferTags: []string{"ws"}, NowS: intPtr(now)},
	)
	if !ok {
		t.Fatalf("ReserveAny returned no lease")
	}
	if lease.Token != "ws-token" || lease.ModeID != -1 || lease.SelectedAt != now {
		t.Fatalf("reserve_any lease = %#v", lease)
	}
	if table.InflightByIdx[idx] != 1 || table.LastUseAtByIdx[idx] != now {
		t.Fatalf("reserve_any counters inflight=%d lastUse=%d", table.InflightByIdx[idx], table.LastUseAtByIdx[idx])
	}
}

func TestAccountDirectoryFeedbackDispatchMatchesPython(t *testing.T) {
	mustSetDirectoryStrategy(t, "quota")
	table, idx := feedbackTable()
	directory := accountDirectoryWithTable(table)
	remaining, resetAtMS, now := 6, 123456, 777
	directory.Feedback("tok", controlaccount.FeedbackKindSuccess, 1, FeedbackOptions{
		Remaining: intPtr(remaining),
		ResetAtMS: intPtr(resetAtMS),
		NowS:      intPtr(now),
	})
	if table.QuotaFastByIdx[idx] != remaining || table.ResetFastAtByIdx[idx] != 123 {
		t.Fatalf("success quota/header update quota=%d reset=%d", table.QuotaFastByIdx[idx], table.ResetFastAtByIdx[idx])
	}

	table, idx = feedbackTable()
	directory = accountDirectoryWithTable(table)
	directory.Feedback("tok", controlaccount.FeedbackKindRateLimited, 1, FeedbackOptions{NowS: intPtr(now)})
	if table.QuotaFastByIdx[idx] != 0 || table.LastFailAtByIdx[idx] != now {
		t.Fatalf("quota rate-limit quota=%d lastFail=%d", table.QuotaFastByIdx[idx], table.LastFailAtByIdx[idx])
	}

	table, idx = feedbackTable()
	directory = accountDirectoryWithTable(table)
	directory.Feedback("tok", controlaccount.FeedbackKindUnauthorized, 1, FeedbackOptions{NowS: intPtr(now)})
	if table.StatusByIdx[idx] != StatusExpired || table.LastFailAtByIdx[idx] != now {
		t.Fatalf("unauthorized status=%d lastFail=%d", table.StatusByIdx[idx], table.LastFailAtByIdx[idx])
	}

	for _, kind := range []controlaccount.FeedbackKind{
		controlaccount.FeedbackKindForbidden,
		controlaccount.FeedbackKindServerError,
	} {
		table, idx = feedbackTable()
		directory = accountDirectoryWithTable(table)
		directory.Feedback("tok", kind, 1, FeedbackOptions{NowS: intPtr(now)})
		if table.LastFailAtByIdx[idx] != now {
			t.Fatalf("%s lastFail=%d", kind, table.LastFailAtByIdx[idx])
		}
	}

	for _, kind := range []controlaccount.FeedbackKind{
		controlaccount.FeedbackKindDisable,
		controlaccount.FeedbackKindDelete,
		controlaccount.FeedbackKindRestore,
	} {
		table, idx = feedbackTable()
		directory = accountDirectoryWithTable(table)
		directory.Feedback("tok", kind, 1, FeedbackOptions{NowS: intPtr(now)})
		if table.StatusByIdx[idx] != StatusActive || table.LastFailAtByIdx[idx] != 0 {
			t.Fatalf("%s should be a directory feedback no-op: status=%d lastFail=%d", kind, table.StatusByIdx[idx], table.LastFailAtByIdx[idx])
		}
	}

	mustSetDirectoryStrategy(t, "random")
	oldConfig := directoryConfigSource
	directoryConfigSource = fakeDirectoryConfig{"account.refresh.basic_interval_sec": 17}
	t.Cleanup(func() { directoryConfigSource = oldConfig })
	table, idx = feedbackTable()
	directory = accountDirectoryWithTable(table)
	before := int(appruntime.NowS())
	directory.Feedback("tok", controlaccount.FeedbackKindRateLimited, 1, FeedbackOptions{NowS: intPtr(now)})
	if table.CoolingUntilSByIdx[idx] < before+17 || table.LastFailAtByIdx[idx] != now {
		t.Fatalf("random rate-limit cooling=%d lastFail=%d", table.CoolingUntilSByIdx[idx], table.LastFailAtByIdx[idx])
	}
}

func TestGetAccountDirectorySingletonMatchesPython(t *testing.T) {
	ctx := context.Background()
	oldDirectory := accountDirectorySingleton
	accountDirectorySingleton = nil
	t.Cleanup(func() { accountDirectorySingleton = oldDirectory })

	if _, err := GetAccountDirectory(ctx, nil); err == nil {
		t.Fatalf("first GetAccountDirectory without repository returned nil error")
	}

	repo := &syncFakeRepository{snapshot: controlaccount.RuntimeSnapshot{
		Revision: 4,
		Items: []controlaccount.AccountRecord{
			accountRecord("singleton", "basic", basicQuotaSet(5), nil),
		},
	}}
	first, err := GetAccountDirectory(ctx, repo)
	if err != nil {
		t.Fatalf("GetAccountDirectory returned error: %v", err)
	}
	if first.Size() != 1 || first.Revision() != 4 {
		t.Fatalf("singleton diagnostics = size %d revision %d", first.Size(), first.Revision())
	}
	second, err := GetAccountDirectory(ctx, nil)
	if err != nil {
		t.Fatalf("second GetAccountDirectory returned error: %v", err)
	}
	if first != second {
		t.Fatalf("singleton was not reused")
	}
}

type fakeDirectoryConfig map[string]int

func (f fakeDirectoryConfig) GetInt(key string, defaultValue int) int {
	if value, ok := f[key]; ok {
		return value
	}
	return defaultValue
}

func accountDirectoryWithTable(table *AccountRuntimeTable) *AccountDirectory {
	return &AccountDirectory{table: table}
}

func mustSetDirectoryStrategy(t *testing.T, strategy string) {
	t.Helper()
	previous := CurrentStrategy()
	if err := SetStrategy(strategy); err != nil {
		t.Fatalf("SetStrategy(%q) returned error: %v", strategy, err)
	}
	t.Cleanup(func() {
		if err := SetStrategy(previous); err != nil {
			t.Fatalf("restore strategy %q returned error: %v", previous, err)
		}
	})
}

func intPtr(value int) *int {
	return &value
}
