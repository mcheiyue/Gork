package account

import "testing"

func TestSelectorStrategyRegistryMatchesPython(t *testing.T) {
	SetStrategy("random")
	if got := CurrentStrategy(); got != "random" {
		t.Fatalf("default/current strategy = %q", got)
	}
	if err := SetStrategy("quota"); err != nil {
		t.Fatalf("SetStrategy(quota) returned error: %v", err)
	}
	if got := CurrentStrategy(); got != "quota" {
		t.Fatalf("current strategy = %q", got)
	}
	if err := SetStrategy("bogus"); err == nil {
		t.Fatal("SetStrategy accepted an unknown strategy")
	}
	if got := CurrentStrategy(); got != "quota" {
		t.Fatalf("invalid strategy changed current strategy to %q", got)
	}
}

func TestQuotaSelectResetsBasicWindowFiltersAndPrefersTags(t *testing.T) {
	table := MakeEmptyTable()
	resetIdx := table.AppendSlot(AccountSlot{
		Token:      "reset",
		PoolID:     0,
		StatusID:   StatusActive,
		QuotaFast:  0,
		TotalFast:  5,
		WindowFast: 10,
		ResetFast:  90,
		Health:     0.9,
		Tags:       []string{"preferred"},
	})
	otherIdx := table.AppendSlot(AccountSlot{
		Token:      "other",
		PoolID:     0,
		StatusID:   StatusActive,
		QuotaFast:  9,
		TotalFast:  9,
		WindowFast: 10,
		ResetFast:  0,
		Health:     1.0,
		Tags:       []string{"other"},
	})
	if err := SetStrategy("quota"); err != nil {
		t.Fatalf("SetStrategy returned error: %v", err)
	}

	got, ok := Select(table, 0, 1, SelectOptions{
		PreferTagIdxs: map[int]bool{resetIdx: true},
		NowS:          100,
	})
	if !ok || got != resetIdx {
		t.Fatalf("Select with preferred tag = %d/%v, want %d/true", got, ok, resetIdx)
	}
	if table.QuotaFastByIdx[resetIdx] != 5 || table.ResetFastAtByIdx[resetIdx] != 110 {
		t.Fatalf("reset window = quota %d reset %d", table.QuotaFastByIdx[resetIdx], table.ResetFastAtByIdx[resetIdx])
	}
	got, ok = Select(table, 0, 1, SelectOptions{
		ExcludeIdxs: map[int]bool{resetIdx: true},
		NowS:        100,
	})
	if !ok || got != otherIdx {
		t.Fatalf("Select with exclude = %d/%v, want %d/true", got, ok, otherIdx)
	}
	table.QuotaFastByIdx[resetIdx] = 0
	table.QuotaFastByIdx[otherIdx] = 0
	if got, ok = Select(table, 0, 1, SelectOptions{NowS: 100}); ok {
		t.Fatalf("Select returned zero-quota candidate %d", got)
	}
}

func TestQuotaSelectScoresAndSelectAnyIgnoresQuota(t *testing.T) {
	table := MakeEmptyTable()
	lowHealth := table.AppendSlot(AccountSlot{Token: "quota", PoolID: 1, StatusID: StatusActive, QuotaAuto: 5, WindowAuto: 60, Health: 0.1})
	highHealth := table.AppendSlot(AccountSlot{Token: "health", PoolID: 1, StatusID: StatusActive, QuotaAuto: 1, WindowAuto: 60, Health: 1.0})
	table.InflightByIdx[highHealth] = 6
	if err := SetStrategy("quota"); err != nil {
		t.Fatalf("SetStrategy returned error: %v", err)
	}
	got, ok := Select(table, 1, 0, SelectOptions{NowS: 100})
	if !ok || got != lowHealth {
		t.Fatalf("quota scoring = %d/%v, want %d/true", got, ok, lowHealth)
	}
	table.QuotaAutoByIdx[lowHealth] = 0
	table.QuotaAutoByIdx[highHealth] = 0
	got, ok = SelectAny(table, 1, SelectOptions{NowS: 100})
	if !ok || got != lowHealth {
		t.Fatalf("SelectAny should ignore quota and prefer low inflight: %d/%v", got, ok)
	}
}

func TestRandomSelectFiltersCoolingInflightExcludeAndPreferTags(t *testing.T) {
	table := MakeEmptyTable()
	cooling := table.AppendSlot(AccountSlot{Token: "cooling", PoolID: 0, StatusID: StatusActive, QuotaFast: 1, WindowFast: 60, Health: 1, Tags: []string{"preferred"}})
	busy := table.AppendSlot(AccountSlot{Token: "busy", PoolID: 0, StatusID: StatusActive, QuotaFast: 1, WindowFast: 60, Health: 1})
	winner := table.AppendSlot(AccountSlot{Token: "winner", PoolID: 0, StatusID: StatusActive, QuotaFast: 1, WindowFast: 60, Health: 1, Tags: []string{"preferred"}})
	excluded := table.AppendSlot(AccountSlot{Token: "excluded", PoolID: 0, StatusID: StatusActive, QuotaFast: 1, WindowFast: 60, Health: 1})
	table.CoolingUntilSByIdx[cooling] = 200
	table.InflightByIdx[busy] = 8
	if err := SetStrategy("random"); err != nil {
		t.Fatalf("SetStrategy returned error: %v", err)
	}
	got, ok := Select(table, 0, 1, SelectOptions{
		ExcludeIdxs:   map[int]bool{excluded: true},
		PreferTagIdxs: map[int]bool{cooling: true, winner: true},
		NowS:          100,
	})
	if !ok || got != winner {
		t.Fatalf("random select = %d/%v, want %d/true", got, ok, winner)
	}
	table.CoolingUntilSByIdx[winner] = 200
	if got, ok = SelectAny(table, 0, SelectOptions{
		ExcludeIdxs: map[int]bool{excluded: true},
		NowS:        100,
	}); ok {
		t.Fatalf("random SelectAny returned unavailable candidate %d", got)
	}
}

func TestQuotaSelectAppliesRecentPenaltyAndFailureCap(t *testing.T) {
	table := MakeEmptyTable()
	recent := table.AppendSlot(AccountSlot{Token: "recent", PoolID: 1, StatusID: StatusActive, QuotaAuto: 2, WindowAuto: 60, Health: 1.0, LastUseS: 99})
	old := table.AppendSlot(AccountSlot{Token: "old", PoolID: 1, StatusID: StatusActive, QuotaAuto: 2, WindowAuto: 60, Health: 1.0, LastUseS: 70})
	if err := SetStrategy("quota"); err != nil {
		t.Fatalf("SetStrategy returned error: %v", err)
	}
	got, ok := Select(table, 1, 0, SelectOptions{NowS: 100})
	if !ok || got != old {
		t.Fatalf("recent penalty selection = %d/%v, want %d/true; recent=%d", got, ok, old, recent)
	}

	capped := table.AppendSlot(AccountSlot{Token: "capped", PoolID: 1, StatusID: StatusActive, QuotaAuto: 6, WindowAuto: 60, Health: 1.0, FailCount: 100})
	got, ok = Select(table, 1, 0, SelectOptions{
		ExcludeIdxs: map[int]bool{recent: true, old: true},
		NowS:        100,
	})
	if !ok || got != capped {
		t.Fatalf("failure cap selection = %d/%v, want %d/true", got, ok, capped)
	}
}
