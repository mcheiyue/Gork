package protocol

import (
	"context"
	"errors"
	"reflect"
	"testing"

	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
	platform "github.com/jiujiu532/grok2api/app/platform"
)

func TestUsagePayloadParserAndQuotaWindowMatchPython(t *testing.T) {
	if string(BuildUsagePayload("fast")) != `{"modelName":"fast"}` {
		t.Fatalf("usage payload mismatch: %s", BuildUsagePayload("fast"))
	}
	parsed := ParseRateLimits(map[string]any{"remainingQueries": float64(3), "totalQueries": float64(5)}, 72000)
	wantParsed := &RateLimitQuota{Remaining: 3, Total: 5, WindowSeconds: 72000}
	if !reflect.DeepEqual(wantParsed, parsed) {
		t.Fatalf("parsed quota mismatch\nwant: %#v\n got: %#v", wantParsed, parsed)
	}
	parsed = ParseRateLimits(map[string]any{"remainingQueries": float64(2), "windowSizeSeconds": float64(3600)}, 72000)
	if !reflect.DeepEqual(&RateLimitQuota{Remaining: 2, Total: 2, WindowSeconds: 3600}, parsed) {
		t.Fatalf("default total/window mismatch: %#v", parsed)
	}
	parsed = ParseRateLimits(map[string]any{"remainingQueries": "3", "totalQueries": "5", "windowSizeSeconds": "0"}, 72000)
	if !reflect.DeepEqual(&RateLimitQuota{Remaining: 3, Total: 5, WindowSeconds: 0}, parsed) {
		t.Fatalf("string numeric quota mismatch: %#v", parsed)
	}
	parsed = ParseRateLimits(map[string]any{"remainingQueries": float64(3.9), "totalQueries": float64(5.8), "windowSizeSeconds": float64(60.7)}, 72000)
	if !reflect.DeepEqual(&RateLimitQuota{Remaining: 3, Total: 5, WindowSeconds: 60}, parsed) {
		t.Fatalf("float truncation quota mismatch: %#v", parsed)
	}
	if ParseRateLimits(map[string]any{}, 72000) != nil {
		t.Fatalf("missing remaining should return nil")
	}
	window := ToUsageQuotaWindow(*wantParsed, 1000)
	if !reflect.DeepEqual(UsageQuotaWindow{Remaining: 3, Total: 5, WindowSeconds: 72000, ResetAt: 72001000, SyncedAt: 1000, Source: UsageQuotaSourceReal}, window) {
		t.Fatalf("quota window mismatch: %#v", window)
	}
}

func TestUsageInvalidCredentialAndFeedbackMappingMatchPython(t *testing.T) {
	for _, body := range []string{"invalid-credentials", "bad-credentials", "failed to look up session id", "blocked-user", "email-domain-rejected", "session not found", "account suspended", "token revoked", "token expired"} {
		if !IsInvalidCredentialsBody(body) {
			t.Fatalf("body should be invalid credentials: %s", body)
		}
	}
	invalid := platform.NewUpstreamError("bad", 403, "token expired")
	if !IsInvalidCredentialsError(invalid) {
		t.Fatalf("upstream invalid credentials not detected")
	}
	if UsageProxyFeedbackKindForError(invalid, 403) != controlproxy.ProxyFeedbackForbidden {
		t.Fatalf("invalid credentials should map to forbidden")
	}
	if UsageProxyFeedbackKindForError(platform.NewUpstreamError("rate", 429, ""), 429) != controlproxy.ProxyFeedbackRateLimited {
		t.Fatalf("429 should map to rate_limited")
	}
	if UsageProxyFeedbackKindForError(platform.NewUpstreamError("challenge", 403, ""), 403) != controlproxy.ProxyFeedbackChallenge {
		t.Fatalf("403 should map to challenge")
	}
	if UsageProxyFeedbackKindForError(platform.NewUpstreamError("unauth", 401, ""), 401) != controlproxy.ProxyFeedbackUnauthorized {
		t.Fatalf("401 should map to unauthorized")
	}
	if UsageProxyFeedbackKindForError(platform.NewUpstreamError("up", 500, ""), 500) != controlproxy.ProxyFeedbackUpstream5xx {
		t.Fatalf("500 should map to upstream_5xx")
	}
	if UsageProxyFeedbackKindForError(errors.New("dial"), 0) != controlproxy.ProxyFeedbackTransportError {
		t.Fatalf("fallback should map to transport_error")
	}
}

func TestFetchAllUsageQuotasUsesModeDefaultsAndPropagatesInvalidCredentials(t *testing.T) {
	fetcher := fakeUsageFetcher{bodies: map[string]map[string]any{
		"auto": {"remainingQueries": float64(1), "totalQueries": float64(2)},
		"fast": {"remainingQueries": float64(3), "windowSizeSeconds": float64(60)},
	}}
	windows, err := FetchAllUsageQuotas(context.Background(), "tok", []int{0, 1, 99}, UsageFetchOptions{Fetcher: fetcher, SyncedAt: 100})
	if err != nil {
		t.Fatalf("FetchAllUsageQuotas returned error: %v", err)
	}
	want := map[int]UsageQuotaWindow{
		0:  {Remaining: 1, Total: 2, WindowSeconds: 7200, ResetAt: 7200100, SyncedAt: 100, Source: UsageQuotaSourceReal},
		1:  {Remaining: 3, Total: 3, WindowSeconds: 60, ResetAt: 60100, SyncedAt: 100, Source: UsageQuotaSourceReal},
		99: {Remaining: 1, Total: 2, WindowSeconds: 72000, ResetAt: 72000100, SyncedAt: 100, Source: UsageQuotaSourceReal},
	}
	if !reflect.DeepEqual(want, windows) {
		t.Fatalf("windows mismatch\nwant: %#v\n got: %#v", want, windows)
	}

	invalid := platform.NewUpstreamError("bad", 403, "blocked-user")
	_, err = FetchAllUsageQuotas(context.Background(), "tok", []int{1}, UsageFetchOptions{Fetcher: fakeUsageFetcher{err: invalid}, SyncedAt: 100})
	if !errors.Is(err, invalid) {
		t.Fatalf("invalid credentials error should propagate, got %#v", err)
	}
}

func TestFetchAllUsageQuotasDefaultModesMatchPythonMaps(t *testing.T) {
	fetcher := fakeUsageFetcher{bodies: map[string]map[string]any{
		"auto":                     {"remainingQueries": float64(10)},
		"fast":                     {"remainingQueries": float64(11)},
		"expert":                   {"remainingQueries": float64(12)},
		"heavy":                    {"remainingQueries": float64(13)},
		"grok-420-computer-use-sa": {"remainingQueries": float64(14)},
	}}
	windows, err := FetchAllUsageQuotas(context.Background(), "tok", nil, UsageFetchOptions{Fetcher: fetcher, SyncedAt: 500})
	if err != nil {
		t.Fatalf("FetchAllUsageQuotas returned error: %v", err)
	}
	want := map[int]UsageQuotaWindow{
		0: {Remaining: 10, Total: 10, WindowSeconds: 7200, ResetAt: 7200500, SyncedAt: 500, Source: UsageQuotaSourceReal},
		1: {Remaining: 11, Total: 11, WindowSeconds: 86400, ResetAt: 86400500, SyncedAt: 500, Source: UsageQuotaSourceReal},
		2: {Remaining: 12, Total: 12, WindowSeconds: 7200, ResetAt: 7200500, SyncedAt: 500, Source: UsageQuotaSourceReal},
		3: {Remaining: 13, Total: 13, WindowSeconds: 7200, ResetAt: 7200500, SyncedAt: 500, Source: UsageQuotaSourceReal},
		4: {Remaining: 14, Total: 14, WindowSeconds: 7200, ResetAt: 7200500, SyncedAt: 500, Source: UsageQuotaSourceReal},
	}
	if !reflect.DeepEqual(want, windows) {
		t.Fatalf("default mode windows mismatch\nwant: %#v\n got: %#v", want, windows)
	}
}

type fakeUsageFetcher struct {
	bodies map[string]map[string]any
	err    error
}

func (f fakeUsageFetcher) FetchUsage(ctx context.Context, token, modeName string) (map[string]any, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.bodies[modeName], nil
}
