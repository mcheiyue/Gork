package reverse

import (
	"testing"

	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
)

func TestBuildAccountFeedbackMatchesPythonSuccess(t *testing.T) {
	patch := BuildAccountFeedback("token-1", NewReverseResult(ResultCategorySuccess), AccountFeedbackOptions{
		Clock: func() int64 { return 123 },
	})

	if patch.Token != "token-1" {
		t.Fatalf("token = %q, want token-1", patch.Token)
	}
	if patch.UsageUseDelta == nil || *patch.UsageUseDelta != 1 {
		t.Fatalf("usage use delta = %#v, want 1", patch.UsageUseDelta)
	}
	if patch.LastUseAt == nil || *patch.LastUseAt != 123 {
		t.Fatalf("last use at = %#v, want 123", patch.LastUseAt)
	}
	if patch.UsageFailDelta != nil || patch.LastFailAt != nil || patch.LastFailReason != nil {
		t.Fatalf("success patch should not set failure fields: %#v", patch)
	}
}

func TestBuildAccountFeedbackMatchesPythonRateLimitedModeQuota(t *testing.T) {
	patch := BuildAccountFeedback("token-2", NewReverseResult(ResultCategoryRateLimited), AccountFeedbackOptions{
		ModeID: 2,
		Clock:  func() int64 { return 456 },
	})

	if patch.UsageFailDelta == nil || *patch.UsageFailDelta != 1 {
		t.Fatalf("usage fail delta = %#v, want 1", patch.UsageFailDelta)
	}
	if patch.LastFailAt == nil || *patch.LastFailAt != 456 {
		t.Fatalf("last fail at = %#v, want 456", patch.LastFailAt)
	}
	if patch.LastFailReason == nil || *patch.LastFailReason != "rate_limited (mode=2)" {
		t.Fatalf("last fail reason = %#v", patch.LastFailReason)
	}
	if patch.QuotaExpert == nil || patch.QuotaExpert["remaining"] != 0 {
		t.Fatalf("quota expert = %#v, want remaining 0", patch.QuotaExpert)
	}
	if patch.QuotaAuto != nil || patch.QuotaFast != nil {
		t.Fatalf("only expert quota should be patched: %#v", patch)
	}
}

func TestBuildAccountFeedbackMatchesPythonRateLimitedQuotaModes(t *testing.T) {
	tests := []struct {
		name       string
		modeID     int
		wantReason string
		wantAuto   bool
		wantFast   bool
		wantExpert bool
	}{
		{name: "auto", modeID: 0, wantReason: "rate_limited (mode=0)", wantAuto: true},
		{name: "fast", modeID: 1, wantReason: "rate_limited (mode=1)", wantFast: true},
		{name: "expert", modeID: 2, wantReason: "rate_limited (mode=2)", wantExpert: true},
		{name: "unknown", modeID: 99, wantReason: "rate_limited (mode=99)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patch := BuildAccountFeedback("token-rate", NewReverseResult(ResultCategoryRateLimited), AccountFeedbackOptions{
				ModeID: tt.modeID,
				Clock:  func() int64 { return 111 },
			})
			if patch.LastFailReason == nil || *patch.LastFailReason != tt.wantReason {
				t.Fatalf("last fail reason = %#v, want %q", patch.LastFailReason, tt.wantReason)
			}
			quotaMatches := func(quota map[string]any, want bool) bool {
				if !want {
					return quota == nil
				}
				return quota != nil && quota["remaining"] == 0
			}
			if !quotaMatches(patch.QuotaAuto, tt.wantAuto) ||
				!quotaMatches(patch.QuotaFast, tt.wantFast) ||
				!quotaMatches(patch.QuotaExpert, tt.wantExpert) {
				t.Fatalf("quota fields = auto:%#v fast:%#v expert:%#v", patch.QuotaAuto, patch.QuotaFast, patch.QuotaExpert)
			}
		})
	}
}

func TestBuildAccountFeedbackMatchesPythonGenericFailure(t *testing.T) {
	result := NewReverseResult(ResultCategoryForbidden)
	result.Error = "blocked"
	patch := BuildAccountFeedback("token-3", result, AccountFeedbackOptions{
		Clock: func() int64 { return 789 },
	})

	if patch.UsageFailDelta == nil || *patch.UsageFailDelta != 1 {
		t.Fatalf("usage fail delta = %#v, want 1", patch.UsageFailDelta)
	}
	if patch.LastFailAt == nil || *patch.LastFailAt != 789 {
		t.Fatalf("last fail at = %#v, want 789", patch.LastFailAt)
	}
	if patch.LastFailReason == nil || *patch.LastFailReason != "blocked" {
		t.Fatalf("last fail reason = %#v, want blocked", patch.LastFailReason)
	}
}

func TestBuildAccountFeedbackUsesPythonCategoryNameWhenErrorMissing(t *testing.T) {
	result := NewReverseResult(ResultCategoryForbidden)
	patch := BuildAccountFeedback("token-4", result, AccountFeedbackOptions{
		Clock: func() int64 { return 790 },
	})

	if patch.LastFailReason == nil || *patch.LastFailReason != "FORBIDDEN" {
		t.Fatalf("last fail reason = %#v, want FORBIDDEN", patch.LastFailReason)
	}
}

func TestBuildProxyFeedbackMatchesPythonCategoryMapping(t *testing.T) {
	tests := []struct {
		category ResultCategory
		want     controlproxy.ProxyFeedbackKind
	}{
		{category: ResultCategorySuccess, want: controlproxy.ProxyFeedbackSuccess},
		{category: ResultCategoryRateLimited, want: controlproxy.ProxyFeedbackRateLimited},
		{category: ResultCategoryAuthFailure, want: controlproxy.ProxyFeedbackUnauthorized},
		{category: ResultCategoryForbidden, want: controlproxy.ProxyFeedbackChallenge},
		{category: ResultCategoryUpstream5xx, want: controlproxy.ProxyFeedbackUpstream5xx},
		{category: ResultCategoryTransportErr, want: controlproxy.ProxyFeedbackTransportError},
		{category: ResultCategoryNotFound, want: controlproxy.ProxyFeedbackTransportError},
		{category: ResultCategoryUnknown, want: controlproxy.ProxyFeedbackTransportError},
	}

	for _, tt := range tests {
		result := NewReverseResult(tt.category)
		result.StatusCode = 499
		result.Error = "reason"
		got := BuildProxyFeedback(result)
		if got.Kind != tt.want {
			t.Fatalf("category %v kind = %v, want %v", tt.category, got.Kind, tt.want)
		}
		if got.StatusCode == nil || *got.StatusCode != 499 {
			t.Fatalf("status code = %#v, want 499", got.StatusCode)
		}
		if got.Reason != "reason" {
			t.Fatalf("reason = %q, want reason", got.Reason)
		}
	}
}
