package account

import "testing"

func stateMS(value int64) *int64 {
	return &value
}

func baseStateRecord(status AccountStatus) AccountRecord {
	return AccountRecord{
		Token:          "tok",
		Pool:           "basic",
		Status:         status,
		Quota:          DefaultQuotaSet("basic").ToDict(),
		Ext:            map[string]any{},
		UsageUseCount:  1,
		UsageFailCount: 2,
		UsageSyncCount: 3,
	}
}

func TestAccountFeedbackFromStatusCode(t *testing.T) {
	tests := []struct {
		status int
		want   FeedbackKind
	}{
		{status: 200, want: FeedbackKindSuccess},
		{status: 299, want: FeedbackKindSuccess},
		{status: 401, want: FeedbackKindUnauthorized},
		{status: 403, want: FeedbackKindForbidden},
		{status: 429, want: FeedbackKindRateLimited},
		{status: 500, want: FeedbackKindServerError},
		{status: 418, want: FeedbackKindServerError},
	}
	for _, tt := range tests {
		got := AccountFeedbackFromStatusCode(tt.status, 2, FeedbackFromStatusOptions{
			Reason:         "reason",
			RetryAfterMS:   stateMS(123),
			ConfirmExpired: true,
		})
		if got.Kind != tt.want || got.ModeID != 2 || got.StatusCode == nil || *got.StatusCode != tt.status {
			t.Fatalf("AccountFeedbackFromStatusCode(%d) = %#v, want kind %q mode/status", tt.status, got, tt.want)
		}
		if got.Reason != "reason" || got.RetryAfterMS == nil || *got.RetryAfterMS != 123 || !got.ConfirmExpired {
			t.Fatalf("feedback options not preserved: %#v", got)
		}
		if got.ApplyUsage {
			t.Fatal("from status code feedback should default ApplyUsage=false")
		}
	}
}

func TestDerivedStatusSelectabilityAndManageability(t *testing.T) {
	activeQuota := DefaultQuotaSet("basic")
	active := baseStateRecord(AccountStatusActive).WithQuotaSet(activeQuota)
	if got := DeriveStatus(active, 100); got != AccountStatusActive {
		t.Fatalf("DeriveStatus(active) = %q", got)
	}
	if !IsSelectable(active, 1, 100) || !IsManageable(active, 100) {
		t.Fatalf("active record should be selectable/manageable")
	}
	exhaustedQuota := activeQuota
	exhaustedQuota.Fast.Remaining = 0
	exhausted := active.WithQuotaSet(exhaustedQuota)
	if IsSelectable(exhausted, 1, 100) {
		t.Fatal("exhausted quota should not be selectable")
	}
	coolingExpired := active
	coolingExpired.Status = AccountStatusCooling
	coolingExpired.Ext = map[string]any{"cooldown_until": int64(99)}
	if got := DeriveStatus(coolingExpired, 100); got != AccountStatusActive {
		t.Fatalf("expired cooldown status = %q, want active", got)
	}
	coolingLive := coolingExpired
	coolingLive.Ext = map[string]any{"cooldown_until": int64(101)}
	if IsSelectable(coolingLive, 1, 100) || !IsManageable(coolingLive, 100) {
		t.Fatalf("live cooling should be manageable but not selectable")
	}
	deleted := active
	deleted.DeletedAt = stateMS(100)
	if IsSelectable(deleted, 1, 100) || IsManageable(deleted, 100) {
		t.Fatal("deleted record should not be selectable/manageable")
	}
}

func TestApplyFeedbackSuccessAndQuotaWindow(t *testing.T) {
	record := baseStateRecord(AccountStatusActive)
	feedback := AccountFeedback{Kind: FeedbackKindSuccess, ModeID: 1, At: 1000, ApplyUsage: true}
	updated, err := ApplyFeedback(record, feedback, StatePolicy{})
	if err != nil {
		t.Fatalf("ApplyFeedback success returned error: %v", err)
	}
	if updated.UsageUseCount != 2 || updated.LastUseAt == nil || *updated.LastUseAt != 1000 {
		t.Fatalf("success usage fields = %#v", updated)
	}
	quota, _ := updated.QuotaSet()
	if quota.Fast.Remaining != 29 {
		t.Fatalf("success should decrement fast quota to 29, got %d", quota.Fast.Remaining)
	}
	realWindow := QuotaWindow{Remaining: 7, Total: 8, WindowSeconds: 9, Source: QuotaSourceReal}
	withReal, err := ApplyFeedback(record, AccountFeedback{Kind: FeedbackKindSuccess, ModeID: 1, At: 2000, QuotaWindow: &realWindow}, StatePolicy{})
	if err != nil {
		t.Fatalf("ApplyFeedback real quota returned error: %v", err)
	}
	quota, _ = withReal.QuotaSet()
	if quota.Fast.Remaining != 7 || withReal.UsageSyncCount != 4 || withReal.LastSyncAt == nil || *withReal.LastSyncAt != 2000 {
		t.Fatalf("real quota feedback fields = quota %#v record %#v", quota.Fast, withReal)
	}
}

func TestApplyFeedbackStatusTransitions(t *testing.T) {
	policy := StatePolicy{FailThreshold: 5, ForbiddenStrikes: 1, DefaultCoolingMS: 900000}
	tests := []struct {
		name       string
		record     AccountRecord
		feedback   AccountFeedback
		wantStatus AccountStatus
		wantReason string
		wantExtKey string
	}{
		{
			name:       "confirmed unauthorized expires",
			record:     baseStateRecord(AccountStatusActive),
			feedback:   AccountFeedback{Kind: FeedbackKindUnauthorized, At: 1000, Reason: "bad", ConfirmExpired: true},
			wantStatus: AccountStatusExpired,
			wantReason: "bad",
			wantExtKey: "expired_at",
		},
		{
			name:       "forbidden disables",
			record:     baseStateRecord(AccountStatusActive),
			feedback:   AccountFeedback{Kind: FeedbackKindForbidden, At: 2000, Reason: "blocked"},
			wantStatus: AccountStatusDisabled,
			wantReason: "blocked",
			wantExtKey: "disabled_at",
		},
		{
			name:       "rate limited cools",
			record:     baseStateRecord(AccountStatusActive),
			feedback:   AccountFeedback{Kind: FeedbackKindRateLimited, ModeID: 1, At: 3000, Reason: "slow", RetryAfterMS: stateMS(500)},
			wantStatus: AccountStatusCooling,
			wantReason: "slow",
			wantExtKey: "cooldown_until",
		},
		{
			name:       "operator disable",
			record:     baseStateRecord(AccountStatusActive),
			feedback:   AccountFeedback{Kind: FeedbackKindDisable, At: 4000},
			wantStatus: AccountStatusDisabled,
			wantReason: "operator_disabled",
			wantExtKey: "disabled_at",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updated, err := ApplyFeedback(tt.record, tt.feedback, policy)
			if err != nil {
				t.Fatalf("ApplyFeedback returned error: %v", err)
			}
			if updated.Status != tt.wantStatus || updated.StateReason == nil || *updated.StateReason != tt.wantReason {
				t.Fatalf("status/state reason = %q/%v, want %q/%q", updated.Status, updated.StateReason, tt.wantStatus, tt.wantReason)
			}
			if _, ok := updated.Ext[tt.wantExtKey]; !ok {
				t.Fatalf("expected ext key %q in %#v", tt.wantExtKey, updated.Ext)
			}
		})
	}
}

func TestApplyFeedbackFailureAccountingAndNoopKinds(t *testing.T) {
	unauthorized := baseStateRecord(AccountStatusActive)
	updated, err := ApplyFeedback(unauthorized, AccountFeedback{Kind: FeedbackKindUnauthorized, At: 7000, StatusCode: intPtr(401)}, StatePolicy{})
	if err != nil {
		t.Fatalf("unconfirmed unauthorized returned error: %v", err)
	}
	if updated.Status != AccountStatusActive || updated.UsageFailCount != 3 || updated.LastFailAt == nil || *updated.LastFailAt != 7000 {
		t.Fatalf("unconfirmed unauthorized fields = %#v", updated)
	}
	if updated.LastFailReason == nil || *updated.LastFailReason != "401" || updated.Ext["expired_at"] != nil {
		t.Fatalf("unconfirmed unauthorized failure reason/ext = %#v/%#v", updated.LastFailReason, updated.Ext)
	}

	serverError := baseStateRecord(AccountStatusActive)
	updated, err = ApplyFeedback(serverError, AccountFeedback{Kind: FeedbackKindServerError, At: 7100, StatusCode: intPtr(503)}, StatePolicy{})
	if err != nil {
		t.Fatalf("server error returned error: %v", err)
	}
	if updated.Status != AccountStatusActive || updated.UsageFailCount != 3 || updated.LastFailReason == nil || *updated.LastFailReason != "503" {
		t.Fatalf("server error failure accounting = %#v", updated)
	}

	deleted := baseStateRecord(AccountStatusActive)
	updated, err = ApplyFeedback(deleted, AccountFeedback{Kind: FeedbackKindDelete, At: 7200}, StatePolicy{})
	if err != nil {
		t.Fatalf("delete feedback returned error: %v", err)
	}
	if updated.Status != AccountStatusActive || updated.UsageFailCount != deleted.UsageFailCount || updated.LastFailAt != nil || updated.UpdatedAt != 7200 {
		t.Fatalf("delete feedback should only update timestamp, got %#v", updated)
	}
}

func TestApplyFeedbackSuccessRecoversExpiredCooldown(t *testing.T) {
	record := baseStateRecord(AccountStatusCooling)
	record.Ext = map[string]any{
		"cooldown_until":    int64(999),
		"cooldown_reason":   "rate_limited",
		"forbidden_strikes": 1,
		"keep":              "value",
	}
	record.StateReason = stringPtr("rate_limited")

	updated, err := ApplyFeedback(record, AccountFeedback{Kind: FeedbackKindSuccess, ModeID: 1, At: 1000, ApplyUsage: false}, StatePolicy{})
	if err != nil {
		t.Fatalf("success recovery returned error: %v", err)
	}
	if updated.Status != AccountStatusActive || updated.StateReason != nil || updated.UsageUseCount != record.UsageUseCount {
		t.Fatalf("success recovery fields = %#v", updated)
	}
	if _, ok := updated.Ext["keep"]; !ok || updated.Ext["cooldown_until"] != nil || updated.Ext["cooldown_reason"] != nil || updated.Ext["forbidden_strikes"] != nil {
		t.Fatalf("success recovery ext cleanup = %#v", updated.Ext)
	}
}

func TestApplyFeedbackRestoreAndClearFailures(t *testing.T) {
	record := baseStateRecord(AccountStatusDisabled)
	record.Ext = map[string]any{
		"cooldown_until":    int64(10),
		"cooldown_reason":   "rate_limited",
		"disabled_at":       int64(11),
		"disabled_reason":   "blocked",
		"expired_at":        int64(12),
		"expired_reason":    "bad",
		"forbidden_strikes": 1,
		"keep":              "value",
	}
	record.StateReason = stringPtr("blocked")
	restored, err := ApplyFeedback(record, AccountFeedback{Kind: FeedbackKindRestore, At: 5000}, StatePolicy{})
	if err != nil {
		t.Fatalf("restore returned error: %v", err)
	}
	if restored.Status != AccountStatusActive || restored.StateReason != nil {
		t.Fatalf("restore status/state reason = %#v/%#v", restored.Status, restored.StateReason)
	}
	if _, ok := restored.Ext["keep"]; !ok || restored.Ext["disabled_at"] != nil || restored.Ext["forbidden_strikes"] != nil {
		t.Fatalf("restore ext cleanup = %#v", restored.Ext)
	}
	quota, _ := restored.QuotaSet()
	if quota.Fast.Total != 30 || quota.Fast.Remaining != 30 {
		t.Fatalf("restore should reset default quota, got %#v", quota.Fast)
	}

	oldNow := stateMachineNowMS
	stateMachineNowMS = func() int64 { return 6000 }
	t.Cleanup(func() { stateMachineNowMS = oldNow })
	cleared := ClearFailures(record)
	if cleared.Status != AccountStatusActive || cleared.UsageFailCount != 0 || cleared.LastFailAt != nil || cleared.LastFailReason != nil || cleared.StateReason != nil || cleared.UpdatedAt != 6000 {
		t.Fatalf("clear failures result = %#v", cleared)
	}
	if _, ok := cleared.Ext["keep"]; !ok || cleared.Ext["cooldown_until"] != nil || cleared.Ext["disabled_at"] != nil {
		t.Fatalf("clear failures ext = %#v", cleared.Ext)
	}
}

func stringPtr(value string) *string {
	return &value
}

func intPtr(value int) *int {
	return &value
}
