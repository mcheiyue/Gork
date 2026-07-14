package openai

import (
	"strings"
	"testing"
	"time"

	"github.com/dslzl/gork/app/platform"
)

func TestParseConsole429Info(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		wantPS bool
		wantPM bool
	}{
		{
			name:   "per-minute only",
			body:   `{"code":"resource-exhausted","error":"Too many requests for team <redacted> and model grok-4.3. Your team's rate limit is — Requests per Minute (actual/limit): 1922/60."}`,
			wantPS: false,
			wantPM: true,
		},
		{
			name:   "per-second only",
			body:   `{"code":"resource-exhausted","error":"Too many requests for team <redacted> and model grok-4.3. Your team's rate limit is — Requests per Second (actual/limit): 2/2."}`,
			wantPS: true,
			wantPM: false,
		},
		{
			name:   "both",
			body:   `{"code":"resource-exhausted","error":"Too many requests for team <redacted> and model grok-4.3. Your team's rate limit is — Requests per Second (actual/limit): 2/2, Requests per Minute (actual/limit): 1922/60."}`,
			wantPS: true,
			wantPM: true,
		},
		{
			name:   "per-second hit, per-minute ok",
			body:   `{"code":"resource-exhausted","error":"Too many requests for team <redacted> and model grok-4.3. Your team's rate limit is — Requests per Second (actual/limit): 2/2, Requests per Minute (actual/limit): 0/60."}`,
			wantPS: true,
			wantPM: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := parseConsole429Info(tt.body)
			if info.IsPerSecondHit != tt.wantPS {
				t.Errorf("IsPerSecondHit = %v, want %v", info.IsPerSecondHit, tt.wantPS)
			}
			if info.IsPerMinuteHit != tt.wantPM {
				t.Errorf("IsPerMinuteHit = %v, want %v", info.IsPerMinuteHit, tt.wantPM)
			}
		})
	}
}

func TestConsoleModelCooldownKeySharesMultiAgent(t *testing.T) {
	if consoleModelCooldownKey("grok-4.20-multi-agent-high") != "console:multi-agent" {
		t.Fatalf("high key mismatch")
	}
	if consoleModelCooldownKey("grok-4.20-multi-agent-low") != "console:multi-agent" {
		t.Fatalf("low key mismatch")
	}
	if consoleModelCooldownKey("grok-4.20-0309-console") != "console:grok-4.20-0309-console" {
		t.Fatalf("light key mismatch")
	}
	if consoleModelCooldownKey("") != "console:unknown" {
		t.Fatalf("empty key mismatch")
	}
}

func TestConsoleCooldownBlocksUntilExpiry(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	consoleCooldownNow = func() time.Time { return now }
	t.Cleanup(func() { consoleCooldownNow = time.Now })

	cb := newConsoleTeamCircuitBreaker(75)
	key := consoleModelCooldownKey("grok-4.20-multi-agent-high")
	info := Console429Info{IsPerMinuteHit: true, PerMinuteActual: 60, PerMinuteLimit: 60}

	d := cb.tripModel(key, info)
	if d != 75*time.Second {
		t.Fatalf("cooldown = %v, want 75s", d)
	}
	if !cb.blocked(key) {
		t.Fatal("expected blocked after RPM trip")
	}
	rem := cb.remainingCooldown(key)
	if rem < 74*time.Second || rem > 75*time.Second {
		t.Fatalf("remaining = %v", rem)
	}

	light := consoleModelCooldownKey("grok-4.20-0309-console")
	if cb.blocked(light) {
		t.Fatal("light model must not share multi-agent cooldown")
	}

	now = now.Add(76 * time.Second)
	if cb.blocked(key) {
		t.Fatal("expected unblocked after expiry")
	}
}

func TestConsoleCooldownRPSShorterThanRPM(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	consoleCooldownNow = func() time.Time { return now }
	t.Cleanup(func() { consoleCooldownNow = time.Now })

	cb := newConsoleTeamCircuitBreaker(75)
	key := consoleModelCooldownKey("grok-4.20-0309-console")
	d := cb.tripModel(key, Console429Info{IsPerSecondHit: true, PerSecondActual: 2, PerSecondLimit: 2})
	if d != 3*time.Second {
		t.Fatalf("rps cooldown = %v, want 3s", d)
	}
}

func TestConsoleCooldownOnlyExtends(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	consoleCooldownNow = func() time.Time { return now }
	t.Cleanup(func() { consoleCooldownNow = time.Now })

	cb := newConsoleTeamCircuitBreaker(75)
	key := consoleModelCooldownKey("grok-4.20-multi-agent-medium")
	cb.tripModel(key, Console429Info{IsPerMinuteHit: true, PerMinuteActual: 1, PerMinuteLimit: 1})
	d := cb.tripModel(key, Console429Info{IsPerSecondHit: true, PerSecondActual: 2, PerSecondLimit: 2})
	if d < 70*time.Second {
		t.Fatalf("should keep longer cooldown, got %v", d)
	}
}

func TestConsoleRateLimitCooldownErrorDetails(t *testing.T) {
	info := Console429Info{IsPerMinuteHit: true, PerMinuteActual: 60, PerMinuteLimit: 60}
	err := consoleRateLimitCooldownError("console:multi-agent", 75*time.Second, &info)
	rl, ok := err.(*platform.RateLimitError)
	if !ok || rl == nil || rl.AppError == nil {
		t.Fatalf("want *platform.RateLimitError, got %#v", err)
	}
	if !strings.Contains(rl.Error(), "retry after 75s") {
		t.Fatalf("message = %q", rl.Error())
	}
	if rl.Details["retry_after"] != 75 {
		t.Fatalf("retry_after = %#v", rl.Details["retry_after"])
	}
	if rl.Details["limit_kind"] != "rpm" {
		t.Fatalf("limit_kind = %#v", rl.Details["limit_kind"])
	}
	body := rl.ToDict()["error"].(map[string]any)
	if body["retry_after"] != 75 {
		t.Fatalf("ToDict retry_after = %#v", body["retry_after"])
	}
}
