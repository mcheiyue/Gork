package openai

import (
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dslzl/gork/app/platform"
)

// 可注入时钟，单测用。
var consoleCooldownNow = time.Now

const (
	consoleRPMCooldownSec     = 75
	consoleRPSCooldownSec     = 3
	consoleUnknownCooldownSec = 5
)

type consoleCooldownEntry struct {
	until time.Time
	info  Console429Info
	kind  string // rpm | rps | unknown
}

// consoleTeamCircuitBreaker：按模型键冷却，避免 multi 撞 team RPM 后空转换号。
type consoleTeamCircuitBreaker struct {
	mu             sync.Mutex
	byModel        map[string]consoleCooldownEntry
	rpmCooldownSec int
	rpsCooldownSec int
	unknownSec     int
}

func newConsoleTeamCircuitBreaker(rpmCooldownSec int) *consoleTeamCircuitBreaker {
	if rpmCooldownSec <= 0 {
		rpmCooldownSec = consoleRPMCooldownSec
	}
	return &consoleTeamCircuitBreaker{
		byModel:        make(map[string]consoleCooldownEntry),
		rpmCooldownSec: rpmCooldownSec,
		rpsCooldownSec: consoleRPSCooldownSec,
		unknownSec:     consoleUnknownCooldownSec,
	}
}

// consoleModelCooldownKey 将客户端模型映射到冷却键（multi-agent 家族共享）。
func consoleModelCooldownKey(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return "console:unknown"
	}
	if strings.Contains(m, "multi-agent") {
		return "console:multi-agent"
	}
	return "console:" + m
}

func (cb *consoleTeamCircuitBreaker) blocked(modelKey string) bool {
	return cb.remainingCooldown(modelKey) > 0
}

func (cb *consoleTeamCircuitBreaker) remainingCooldown(modelKey string) time.Duration {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	entry, ok := cb.byModel[modelKey]
	if !ok {
		return 0
	}
	rem := entry.until.Sub(consoleCooldownNow())
	if rem <= 0 {
		delete(cb.byModel, modelKey)
		return 0
	}
	return rem
}

// tripModel 记录 429 并设置/延长冷却，返回本次采用的冷却时长。
func (cb *consoleTeamCircuitBreaker) tripModel(modelKey string, info Console429Info) time.Duration {
	kind, sec := cooldownKindAndSec(info, cb.rpmCooldownSec, cb.rpsCooldownSec, cb.unknownSec)
	d := time.Duration(sec) * time.Second
	until := consoleCooldownNow().Add(d)

	cb.mu.Lock()
	defer cb.mu.Unlock()
	if existing, ok := cb.byModel[modelKey]; ok && existing.until.After(until) {
		d = existing.until.Sub(consoleCooldownNow())
		slog.Warn("console circuit breaker: 429, keep longer cooldown",
			"key", modelKey, "kind", existing.kind, "remaining_sec", int(d.Seconds()))
		return d
	}
	cb.byModel[modelKey] = consoleCooldownEntry{until: until, info: info, kind: kind}
	slog.Warn("console circuit breaker: 429 cooldown armed",
		"key", modelKey, "kind", kind, "cooldown_sec", sec,
		"per_second_hit", info.IsPerSecondHit, "per_minute_hit", info.IsPerMinuteHit)
	return d
}

func cooldownKindAndSec(info Console429Info, rpmSec, rpsSec, unknownSec int) (string, int) {
	if info.IsPerMinuteHit {
		return "rpm", rpmSec
	}
	if info.IsPerSecondHit {
		return "rps", rpsSec
	}
	return "unknown", unknownSec
}

func consoleRateLimitCooldownError(modelKey string, remaining time.Duration, info *Console429Info) error {
	sec := int((remaining + time.Second - 1) / time.Second)
	if sec < 1 {
		sec = 1
	}
	details := map[string]any{
		"retry_after":  sec,
		"cooldown_key": modelKey,
	}
	limitKind := "cooldown"
	if info != nil {
		details["per_second_actual"] = info.PerSecondActual
		details["per_second_limit"] = info.PerSecondLimit
		details["per_minute_actual"] = info.PerMinuteActual
		details["per_minute_limit"] = info.PerMinuteLimit
		switch {
		case info.IsPerMinuteHit:
			limitKind = "rpm"
		case info.IsPerSecondHit:
			limitKind = "rps"
		}
		details["limit_kind"] = limitKind
	}
	msg := fmt.Sprintf("Console rate limit cooldown active, retry after %ds", sec)
	return &platform.RateLimitError{
		AppError: platform.NewAppError(msg, platform.ErrorKindRateLimit, "rate_limit_exceeded", 429, details),
	}
}

// isConsoleRateLimitError checks if the error is a 429 rate limit error.
func isConsoleRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "429") || strings.Contains(msg, "rate_limit") || strings.Contains(msg, "resource-exhausted")
}

// extract429Body extracts the response body from an UpstreamError for 429 parsing.
func extract429Body(err error) string {
	if upstreamErr, ok := err.(*platform.UpstreamError); ok {
		return upstreamErr.Body
	}
	return err.Error()
}

// Console429Info holds parsed information from a 429 response body.
type Console429Info struct {
	PerSecondActual int
	PerSecondLimit  int
	PerMinuteActual int
	PerMinuteLimit  int
	IsPerSecondHit  bool
	IsPerMinuteHit  bool
}

var (
	perSecondPattern = regexp.MustCompile(`Requests per Second \(actual/limit\): (\d+)/(\d+)`)
	perMinutePattern = regexp.MustCompile(`Requests per Minute \(actual/limit\): (\d+)/(\d+)`)
)

// parseConsole429Info parses the 429 response body to determine which rate limit was hit.
func parseConsole429Info(body string) Console429Info {
	var info Console429Info
	if matches := perSecondPattern.FindStringSubmatch(body); len(matches) == 3 {
		info.PerSecondActual, _ = strconv.Atoi(matches[1])
		info.PerSecondLimit, _ = strconv.Atoi(matches[2])
		if info.PerSecondLimit > 0 {
			info.IsPerSecondHit = info.PerSecondActual >= info.PerSecondLimit
		}
	}
	if matches := perMinutePattern.FindStringSubmatch(body); len(matches) == 3 {
		info.PerMinuteActual, _ = strconv.Atoi(matches[1])
		info.PerMinuteLimit, _ = strconv.Atoi(matches[2])
		if info.PerMinuteLimit > 0 {
			info.IsPerMinuteHit = info.PerMinuteActual >= info.PerMinuteLimit
		}
	}
	return info
}

// Global console circuit breaker（按模型键真冷却）
var consoleCircuitBreaker = newConsoleTeamCircuitBreaker(consoleRPMCooldownSec)
