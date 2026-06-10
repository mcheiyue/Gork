package account

import (
	"math"
	"testing"

	appruntime "github.com/jiujiu532/grok2api/app/platform/runtime"
)

func TestApplySuccessAndRateLimitFeedbackMatchesPythonStrategies(t *testing.T) {
	table, idx := feedbackTable()
	ApplySuccessQuota(table, idx, 1)
	if table.QuotaFastByIdx[idx] != 1 || !floatClose(table.HealthByIdx[idx], 1.0) {
		t.Fatalf("success quota: quota=%d health=%f", table.QuotaFastByIdx[idx], table.HealthByIdx[idx])
	}
	ApplySuccessQuota(table, idx, 1)
	ApplySuccessQuota(table, idx, 1)
	if table.QuotaFastByIdx[idx] != 0 {
		t.Fatalf("success quota underflowed to %d", table.QuotaFastByIdx[idx])
	}

	table.HealthByIdx[idx] = 0.5
	table.QuotaFastByIdx[idx] = 7
	ApplySuccessRandom(table, idx)
	if table.QuotaFastByIdx[idx] != 7 || !floatClose(table.HealthByIdx[idx], 0.62) {
		t.Fatalf("success random: quota=%d health=%f", table.QuotaFastByIdx[idx], table.HealthByIdx[idx])
	}

	table.HealthByIdx[idx] = 0.8
	ApplyRateLimitedQuota(table, idx, 1)
	if table.QuotaFastByIdx[idx] != 0 || !floatClose(table.HealthByIdx[idx], 0.36) {
		t.Fatalf("rate quota: quota=%d health=%f", table.QuotaFastByIdx[idx], table.HealthByIdx[idx])
	}

	before := int(appruntime.NowS())
	table.HealthByIdx[idx] = 0.8
	table.CoolingUntilSByIdx[idx] = before + 100
	ApplyRateLimitedRandom(table, idx, 5)
	if table.CoolingUntilSByIdx[idx] != before+100 {
		t.Fatalf("cooling timestamp was lowered to %d", table.CoolingUntilSByIdx[idx])
	}
	table.CoolingUntilSByIdx[idx] = 0
	ApplyRateLimitedRandom(table, idx, 5)
	if table.CoolingUntilSByIdx[idx] < before+5 || !floatClose(table.HealthByIdx[idx], 0.162) {
		t.Fatalf("random rate-limit: cooling=%d health=%f", table.CoolingUntilSByIdx[idx], table.HealthByIdx[idx])
	}
}

func TestApplyStatusChangeRefreshesAvailabilityIndexesLikePython(t *testing.T) {
	table, idx := feedbackTable()
	key := ModeKey{PoolID: 0, ModeID: 1}
	if !table.ModeAvailable[key][idx] {
		t.Fatalf("missing initial mode index: %#v", table.ModeAvailable)
	}
	ApplyStatusChange(table, idx, StatusDisabled)
	if table.StatusByIdx[idx] != StatusDisabled || table.ModeAvailable[key][idx] {
		t.Fatalf("disabled status/index mismatch: status=%d modes=%#v", table.StatusByIdx[idx], table.ModeAvailable)
	}
	table.QuotaFastByIdx[idx] = 2
	ApplyStatusChange(table, idx, StatusActive)
	if table.StatusByIdx[idx] != StatusActive || !table.ModeAvailable[key][idx] {
		t.Fatalf("active status/index mismatch: status=%d modes=%#v", table.StatusByIdx[idx], table.ModeAvailable)
	}
	table.QuotaFastByIdx[idx] = 0
	ApplyStatusChange(table, idx, StatusCooling)
	ApplyStatusChange(table, idx, StatusActive)
	if table.ModeAvailable[key][idx] {
		t.Fatalf("active zero-quota account was indexed: %#v", table.ModeAvailable)
	}
}

func TestApplyQuotaUpdateAndCountersMatchPythonBounds(t *testing.T) {
	table, idx := feedbackTable()
	key := ModeKey{PoolID: 0, ModeID: 1}
	table.ModeAvailable[key] = map[int]bool{}
	ApplyQuotaUpdate(table, idx, 1, 40000, 99)
	if table.QuotaFastByIdx[idx] != 32767 || table.ResetFastAtByIdx[idx] != 99 || !table.ModeAvailable[key][idx] {
		t.Fatalf("quota update mismatch: quota=%d reset=%d modes=%#v", table.QuotaFastByIdx[idx], table.ResetFastAtByIdx[idx], table.ModeAvailable)
	}
	ApplyQuotaUpdate(table, idx, 1, -5, 123)
	if table.QuotaFastByIdx[idx] != 0 || table.ResetFastAtByIdx[idx] != 123 {
		t.Fatalf("quota clamp mismatch: quota=%d reset=%d", table.QuotaFastByIdx[idx], table.ResetFastAtByIdx[idx])
	}
	table.ModeAvailable[key] = map[int]bool{}
	table.StatusByIdx[idx] = StatusDisabled
	ApplyQuotaUpdate(table, idx, 1, 3, 456)
	if table.ModeAvailable[key][idx] {
		t.Fatalf("inactive account was indexed after quota update: %#v", table.ModeAvailable)
	}

	table.InflightByIdx[idx] = 65535
	IncrementInflight(table, idx)
	if table.InflightByIdx[idx] != 65535 {
		t.Fatalf("inflight cap = %d", table.InflightByIdx[idx])
	}
	table.InflightByIdx[idx] = 0
	DecrementInflight(table, idx)
	if table.InflightByIdx[idx] != 0 {
		t.Fatalf("inflight floor = %d", table.InflightByIdx[idx])
	}
	UpdateLastUse(table, idx, 111)
	table.FailCountByIdx[idx] = 65535
	UpdateLastFail(table, idx, 222)
	if table.LastUseAtByIdx[idx] != 111 || table.LastFailAtByIdx[idx] != 222 || table.FailCountByIdx[idx] != 65535 {
		t.Fatalf("last-use/fail mismatch: use=%d fail=%d count=%d", table.LastUseAtByIdx[idx], table.LastFailAtByIdx[idx], table.FailCountByIdx[idx])
	}
}

func TestApplyFailureHealthFactorsMatchPython(t *testing.T) {
	table, idx := feedbackTable()
	tests := []struct {
		name string
		fn   func(*AccountRuntimeTable, int)
		want float64
	}{
		{name: "auth", fn: ApplyAuthFailure, want: 0.44},
		{name: "forbidden", fn: ApplyForbidden, want: 0.2},
		{name: "server", fn: ApplyServerError, want: 0.6},
	}
	for _, tt := range tests {
		table.HealthByIdx[idx] = 0.8
		tt.fn(table, idx)
		if !floatClose(table.HealthByIdx[idx], tt.want) {
			t.Fatalf("%s health = %f, want %f", tt.name, table.HealthByIdx[idx], tt.want)
		}
	}
	table.HealthByIdx[idx] = 0.01
	ApplyForbidden(table, idx)
	if !floatClose(table.HealthByIdx[idx], 0.05) {
		t.Fatalf("minimum health = %f", table.HealthByIdx[idx])
	}
}

func feedbackTable() (*AccountRuntimeTable, int) {
	table := MakeEmptyTable()
	idx := table.AppendSlot(AccountSlot{
		Token:         "tok",
		PoolID:        0,
		StatusID:      StatusActive,
		QuotaFast:     2,
		WindowFast:    60,
		ResetFast:     10,
		Health:        0.9,
		FailCount:     1,
		Tags:          []string{"tag"},
		QuotaConsole:  1,
		WindowConsole: 60,
	})
	return table, idx
}

func floatClose(got, want float64) bool {
	return math.Abs(got-want) < 0.000001
}
