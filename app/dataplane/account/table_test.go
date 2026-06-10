package account

import (
	"reflect"
	"testing"
)

func TestMakeEmptyTableMatchesPythonDefaults(t *testing.T) {
	table := MakeEmptyTable()
	if table.Size != 0 || table.Revision != 0 {
		t.Fatalf("size/revision = %d/%d", table.Size, table.Revision)
	}
	if len(table.TokenByIdx) != 0 || len(table.IdxByToken) != 0 {
		t.Fatalf("non-empty token indexes: %#v %#v", table.TokenByIdx, table.IdxByToken)
	}
	if len(table.ModeAvailable) != 0 || len(table.TagIdx) != 0 {
		t.Fatalf("non-empty secondary indexes: %#v %#v", table.ModeAvailable, table.TagIdx)
	}
}

func TestAppendSlotClampsAndIndexesLikePython(t *testing.T) {
	table := MakeEmptyTable()
	idx := table.AppendSlot(AccountSlot{
		Token:         "tok",
		PoolID:        1,
		StatusID:      StatusActive,
		QuotaAuto:     -5,
		QuotaFast:     40000,
		QuotaExpert:   3,
		QuotaHeavy:    4,
		QuotaGrok43:   5,
		QuotaConsole:  6,
		TotalAuto:     -9,
		TotalFast:     40000,
		WindowAuto:    0,
		WindowFast:    10,
		WindowExpert:  11,
		WindowHeavy:   12,
		WindowGrok43:  13,
		WindowConsole: 14,
		ResetAuto:     101,
		ResetConsole:  202,
		Health:        0.75,
		LastUseS:      111,
		LastFailS:     222,
		FailCount:     70000,
		Tags:          []string{"blue", "canary"},
	})

	if idx != 0 || table.Size != 1 || table.IdxByToken["tok"] != 0 {
		t.Fatalf("idx/size/token = %d/%d/%#v", idx, table.Size, table.IdxByToken)
	}
	if table.QuotaAutoByIdx[0] != -1 || table.QuotaFastByIdx[0] != 32767 {
		t.Fatalf("quota clamp = %d/%d", table.QuotaAutoByIdx[0], table.QuotaFastByIdx[0])
	}
	if table.TotalAutoByIdx[0] != 0 || table.TotalFastByIdx[0] != 32767 {
		t.Fatalf("total clamp = %d/%d", table.TotalAutoByIdx[0], table.TotalFastByIdx[0])
	}
	if table.FailCountByIdx[0] != 65535 {
		t.Fatalf("fail count clamp = %d", table.FailCountByIdx[0])
	}
	if table.GetToken(0) != "tok" || table.GetPoolID(0) != 1 || !table.IsActive(0) {
		t.Fatalf("accessors token=%s pool=%d active=%v", table.GetToken(0), table.GetPoolID(0), table.IsActive(0))
	}
	if table.QuotaFor(0, 999) != 6 {
		t.Fatalf("unknown mode should use console quota, got %d", table.QuotaFor(0, 999))
	}
	if table.TotalFor(0, 999) != 0 || table.WindowFor(0, 999) != 14 || table.ResetFor(0, 999) != 202 {
		t.Fatalf("unknown mode total/window/reset should use console columns, got %d/%d/%d", table.TotalFor(0, 999), table.WindowFor(0, 999), table.ResetFor(0, 999))
	}
	if set := table.ModeAvailable[ModeKey{PoolID: 1, ModeID: 0}]; len(set) != 0 {
		t.Fatalf("zero window mode should not be available: %#v", set)
	}
	for _, modeID := range []int{1, 2, 3, 4, 5} {
		if !table.ModeAvailable[ModeKey{PoolID: 1, ModeID: modeID}][0] {
			t.Fatalf("missing mode index %d: %#v", modeID, table.ModeAvailable)
		}
	}
	if !table.TagIdx["blue"][0] || !table.TagIdx["canary"][0] {
		t.Fatalf("tag indexes=%#v", table.TagIdx)
	}
}

func TestUpdateSlotRefreshesIndexesAndLiveIterationLikePython(t *testing.T) {
	table := MakeEmptyTable()
	idx := table.AppendSlot(AccountSlot{
		Token:        "tok",
		PoolID:       1,
		StatusID:     StatusActive,
		QuotaAuto:    10,
		WindowAuto:   30,
		WindowFast:   40,
		Health:       0.5,
		LastUseS:     10,
		LastFailS:    20,
		FailCount:    1,
		Tags:         []string{"old"},
		ResetAuto:    100,
		ResetFast:    200,
		ResetConsole: 300,
	})
	table.InflightByIdx[idx] = 7
	table.CoolingUntilSByIdx[idx] = 99

	table.UpdateSlot(idx, AccountSlot{
		PoolID:        2,
		StatusID:      StatusCooling,
		QuotaAuto:     20,
		QuotaConsole:  30,
		WindowAuto:    0,
		WindowConsole: 50,
		Health:        0.9,
		LastUseS:      30,
		LastFailS:     40,
		FailCount:     2,
		Tags:          []string{"new"},
		ResetConsole:  555,
	}, []string{"old"})

	if table.ModeAvailable[ModeKey{PoolID: 1, ModeID: 0}][idx] {
		t.Fatalf("old mode index still contains idx: %#v", table.ModeAvailable)
	}
	if table.ModeAvailable[ModeKey{PoolID: 2, ModeID: 5}][idx] {
		t.Fatalf("cooling slot should not be mode-available: %#v", table.ModeAvailable)
	}
	if table.TagIdx["old"][idx] || !table.TagIdx["new"][idx] {
		t.Fatalf("tag indexes=%#v", table.TagIdx)
	}
	if table.InflightByIdx[idx] != 7 || table.CoolingUntilSByIdx[idx] != 99 {
		t.Fatalf("update should preserve inflight/cooling, got %d/%d", table.InflightByIdx[idx], table.CoolingUntilSByIdx[idx])
	}
	if table.IsActive(idx) {
		t.Fatalf("cooling slot reported active")
	}
	if got := table.IterLiveIndices(); !reflect.DeepEqual(got, []int{idx}) {
		t.Fatalf("live indices=%#v", got)
	}

	table.UpdateSlot(idx, AccountSlot{PoolID: 2, StatusID: StatusDeleted, Tags: []string{"new"}}, []string{"new"})
	if got := table.IterLiveIndices(); len(got) != 0 {
		t.Fatalf("deleted slot should not be live: %#v", got)
	}
}
