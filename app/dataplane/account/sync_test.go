package account

import (
	"context"
	"reflect"
	"testing"

	controlaccount "github.com/jiujiu532/grok2api/app/control/account"
)

type syncFakeRepository struct {
	snapshot   controlaccount.RuntimeSnapshot
	changes    []controlaccount.AccountChangeSet
	scanCalls  []int
	scanLimits []int
}

func (r *syncFakeRepository) Initialize(context.Context) error { return nil }
func (r *syncFakeRepository) GetRevision(context.Context) (int, error) {
	return r.snapshot.Revision, nil
}
func (r *syncFakeRepository) RuntimeSnapshot(context.Context) (controlaccount.RuntimeSnapshot, error) {
	return r.snapshot, nil
}
func (r *syncFakeRepository) ScanChanges(_ context.Context, revision int, limit int) (controlaccount.AccountChangeSet, error) {
	r.scanCalls = append(r.scanCalls, revision)
	r.scanLimits = append(r.scanLimits, limit)
	if len(r.changes) == 0 {
		return controlaccount.NewAccountChangeSet(), nil
	}
	next := r.changes[0]
	r.changes = r.changes[1:]
	return next, nil
}
func (r *syncFakeRepository) UpsertAccounts(context.Context, []controlaccount.AccountUpsert) (controlaccount.AccountMutationResult, error) {
	return controlaccount.AccountMutationResult{}, nil
}
func (r *syncFakeRepository) PatchAccounts(context.Context, []controlaccount.AccountPatch) (controlaccount.AccountMutationResult, error) {
	return controlaccount.AccountMutationResult{}, nil
}
func (r *syncFakeRepository) DeleteAccounts(context.Context, []string) (controlaccount.AccountMutationResult, error) {
	return controlaccount.AccountMutationResult{}, nil
}
func (r *syncFakeRepository) GetAccounts(context.Context, []string) ([]controlaccount.AccountRecord, error) {
	return nil, nil
}
func (r *syncFakeRepository) ListAccounts(context.Context, controlaccount.ListAccountsQuery) (controlaccount.AccountPage, error) {
	return controlaccount.AccountPage{}, nil
}
func (r *syncFakeRepository) ReplacePool(context.Context, controlaccount.BulkReplacePoolCommand) (controlaccount.AccountMutationResult, error) {
	return controlaccount.AccountMutationResult{}, nil
}
func (r *syncFakeRepository) Close(context.Context) error { return nil }

func TestRecordToSlotMatchesPythonMapping(t *testing.T) {
	resetAuto, resetFast, resetExpert := int64(123456), int64(234567), int64(345678)
	resetHeavy, resetGrok, resetConsole := int64(456789), int64(567890), int64(678901)
	lastUse, lastFail := int64(2000), int64(3000)
	heavy := controlaccount.QuotaWindow{Remaining: 4, Total: 40, WindowSeconds: 400, ResetAt: &resetHeavy}
	grok := controlaccount.QuotaWindow{Remaining: 5, Total: 50, WindowSeconds: 500, ResetAt: &resetGrok}
	console := controlaccount.QuotaWindow{Remaining: 6, Total: 60, WindowSeconds: 600, ResetAt: &resetConsole}
	record := accountRecord("tok-heavy", "heavy", controlaccount.AccountQuotaSet{
		Auto:    controlaccount.QuotaWindow{Remaining: 1, Total: 10, WindowSeconds: 100, ResetAt: &resetAuto},
		Fast:    controlaccount.QuotaWindow{Remaining: 2, Total: 20, WindowSeconds: 200, ResetAt: &resetFast},
		Expert:  controlaccount.QuotaWindow{Remaining: 3, Total: 30, WindowSeconds: 300, ResetAt: &resetExpert},
		Heavy:   &heavy,
		Grok43:  &grok,
		Console: &console,
	}, []string{"blue", "gold"})
	record.LastUseAt = &lastUse
	record.LastFailAt = &lastFail
	record.UsageFailCount = 9

	slot, tags, err := recordToSlot(record)
	if err != nil {
		t.Fatalf("recordToSlot returned error: %v", err)
	}
	if !reflect.DeepEqual(tags, []string{"blue", "gold"}) {
		t.Fatalf("tags = %#v", tags)
	}
	if slot.PoolID != 2 || slot.StatusID != StatusActive {
		t.Fatalf("pool/status = %d/%d", slot.PoolID, slot.StatusID)
	}
	if slot.QuotaAuto != 1 || slot.QuotaFast != 2 || slot.QuotaExpert != 3 ||
		slot.QuotaHeavy != 4 || slot.QuotaGrok43 != 5 || slot.QuotaConsole != 6 {
		t.Fatalf("quota mapping mismatch: %#v", slot)
	}
	if slot.TotalAuto != 10 || slot.TotalFast != 20 || slot.TotalExpert != 30 ||
		slot.TotalHeavy != 40 || slot.TotalGrok43 != 50 || slot.TotalConsole != 60 {
		t.Fatalf("total mapping mismatch: %#v", slot)
	}
	if slot.WindowAuto != 100 || slot.WindowFast != 200 || slot.WindowExpert != 300 ||
		slot.WindowHeavy != 400 || slot.WindowGrok43 != 500 || slot.WindowConsole != 600 {
		t.Fatalf("window mapping mismatch: %#v", slot)
	}
	if slot.ResetAuto != 123 || slot.ResetFast != 234 || slot.ResetExpert != 345 ||
		slot.ResetHeavy != 456 || slot.ResetGrok43 != 567 || slot.ResetConsole != 678 {
		t.Fatalf("reset mapping mismatch: %#v", slot)
	}
	if slot.LastUseS != 2 || slot.LastFailS != 3 || slot.FailCount != 9 || slot.Health != 1.0 {
		t.Fatalf("runtime counters mismatch: %#v", slot)
	}
}

func TestBootstrapSkipsDeletedAndBuildsRuntimeIndexes(t *testing.T) {
	deletedAt := int64(99)
	repo := &syncFakeRepository{snapshot: controlaccount.RuntimeSnapshot{
		Revision: 7,
		Items: []controlaccount.AccountRecord{
			accountRecord("live", "basic", basicQuotaSet(8), []string{"team-a"}),
			func() controlaccount.AccountRecord {
				deleted := accountRecord("gone", "basic", basicQuotaSet(4), []string{"team-a"})
				deleted.DeletedAt = &deletedAt
				return deleted
			}(),
		},
	}}

	table, err := Bootstrap(context.Background(), repo)
	if err != nil {
		t.Fatalf("Bootstrap returned error: %v", err)
	}
	if table.Revision != 7 || table.Size != 1 {
		t.Fatalf("revision/size = %d/%d", table.Revision, table.Size)
	}
	if _, ok := table.IdxByToken["gone"]; ok {
		t.Fatalf("deleted token was loaded: %#v", table.IdxByToken)
	}
	idx := table.IdxByToken["live"]
	if !table.TagIdx["team-a"][idx] || !table.ModeAvailable[ModeKey{PoolID: 0, ModeID: 1}][idx] {
		t.Fatalf("runtime indexes not built: tags=%#v modes=%#v", table.TagIdx, table.ModeAvailable)
	}
}

func TestApplyChangesDeletesUpdatesAppendsAndAdvancesRevision(t *testing.T) {
	table := MakeEmptyTable()
	table.Revision = 1
	oldIdx := table.AppendSlot(AccountSlot{Token: "old", PoolID: 0, StatusID: StatusActive, QuotaFast: 1, WindowFast: 60, Health: 1, Tags: []string{"old-tag"}})
	keepIdx := table.AppendSlot(AccountSlot{Token: "keep", PoolID: 0, StatusID: StatusActive, QuotaFast: 1, WindowFast: 60, Health: 1, Tags: []string{"old-team"}})
	repo := &syncFakeRepository{changes: []controlaccount.AccountChangeSet{
		{
			Revision:      2,
			DeletedTokens: []string{"old"},
			Items: []controlaccount.AccountRecord{
				accountRecord("keep", "super", superQuotaSet(22), []string{"new-team"}),
				accountRecord("new", "basic", basicQuotaSet(11), []string{"new-team"}),
			},
			HasMore: true,
		},
		{Revision: 3},
	}}

	changed, err := ApplyChanges(context.Background(), table, repo)
	if err != nil {
		t.Fatalf("ApplyChanges returned error: %v", err)
	}
	if !changed {
		t.Fatal("ApplyChanges returned changed=false")
	}
	if table.Revision != 3 || table.Size != 2 {
		t.Fatalf("revision/size = %d/%d", table.Revision, table.Size)
	}
	if table.StatusByIdx[oldIdx] != StatusDeleted || table.ModeAvailable[ModeKey{PoolID: 0, ModeID: 1}][oldIdx] {
		t.Fatalf("deleted token not removed from mode indexes: status=%d modes=%#v", table.StatusByIdx[oldIdx], table.ModeAvailable)
	}
	if table.PoolByIdx[keepIdx] != 1 || table.QuotaAutoByIdx[keepIdx] != 22 {
		t.Fatalf("updated slot mismatch: pool=%d quota=%d", table.PoolByIdx[keepIdx], table.QuotaAutoByIdx[keepIdx])
	}
	if len(table.TagIdx["old-team"]) != 0 || !table.TagIdx["new-team"][keepIdx] {
		t.Fatalf("updated tags mismatch: %#v", table.TagIdx)
	}
	if _, ok := table.IdxByToken["new"]; !ok {
		t.Fatalf("new token was not appended: %#v", table.IdxByToken)
	}
	if !reflect.DeepEqual(repo.scanCalls, []int{1, 2}) {
		t.Fatalf("scan revisions = %#v", repo.scanCalls)
	}
	if !reflect.DeepEqual(repo.scanLimits, []int{defaultSyncBatchLimit, defaultSyncBatchLimit}) {
		t.Fatalf("scan limits = %#v", repo.scanLimits)
	}
}

func TestApplyChangesNoChangesReturnsFalseAndKeepsRevisionSemantics(t *testing.T) {
	table := MakeEmptyTable()
	table.Revision = 4
	repo := &syncFakeRepository{changes: []controlaccount.AccountChangeSet{{Revision: 5}}}

	changed, err := ApplyChanges(context.Background(), table, repo, 17)
	if err != nil {
		t.Fatalf("ApplyChanges returned error: %v", err)
	}
	if changed {
		t.Fatal("ApplyChanges returned changed=true for an empty changeset")
	}
	if table.Revision != 5 {
		t.Fatalf("revision = %d", table.Revision)
	}
	if !reflect.DeepEqual(repo.scanCalls, []int{4}) || !reflect.DeepEqual(repo.scanLimits, []int{17}) {
		t.Fatalf("scan args = revisions %#v limits %#v", repo.scanCalls, repo.scanLimits)
	}
}

func accountRecord(token, pool string, quotaSet controlaccount.AccountQuotaSet, tags []string) controlaccount.AccountRecord {
	return controlaccount.AccountRecord{
		Token:  token,
		Pool:   pool,
		Status: controlaccount.AccountStatusActive,
		Tags:   tags,
		Quota:  quotaSet.ToDict(),
	}
}

func basicQuotaSet(fastRemaining int) controlaccount.AccountQuotaSet {
	return controlaccount.AccountQuotaSet{
		Fast: controlaccount.QuotaWindow{Remaining: fastRemaining, Total: 30, WindowSeconds: 86400},
	}
}

func superQuotaSet(autoRemaining int) controlaccount.AccountQuotaSet {
	return controlaccount.AccountQuotaSet{
		Auto:    controlaccount.QuotaWindow{Remaining: autoRemaining, Total: 50, WindowSeconds: 7200},
		Fast:    controlaccount.QuotaWindow{Remaining: 140, Total: 140, WindowSeconds: 7200},
		Expert:  controlaccount.QuotaWindow{Remaining: 50, Total: 50, WindowSeconds: 7200},
		Grok43:  &controlaccount.QuotaWindow{Remaining: 50, Total: 50, WindowSeconds: 7200},
		Console: &controlaccount.QuotaWindow{Remaining: 30, Total: 30, WindowSeconds: 900},
	}
}
