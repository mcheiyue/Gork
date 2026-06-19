package openai

import (
	"log/slog"
	"strings"
	"sync"
	"time"
)

// consoleTeamCircuitBreaker tracks 429 state for logging/monitoring only.
// Since the rate limit is team-level (60 req/min shared), we do NOT block requests.
// Instead, we retry with random delays to compete for the shared quota.
type consoleTeamCircuitBreaker struct {
	mu          sync.Mutex
	last429Time time.Time
	cooldownSec int
}

func newConsoleTeamCircuitBreaker(cooldownSec int) *consoleTeamCircuitBreaker {
	if cooldownSec <= 0 {
		cooldownSec = 60
	}
	return &consoleTeamCircuitBreaker{
		cooldownSec: cooldownSec,
	}
}

// blocked always returns false - we never block, just log and retry.
func (cb *consoleTeamCircuitBreaker) blocked() bool {
	return false
}

// remainingCooldown always returns 0 - we never block.
func (cb *consoleTeamCircuitBreaker) remainingCooldown() time.Duration {
	return 0
}

// trip records the 429 event for logging, but does NOT block future requests.
func (cb *consoleTeamCircuitBreaker) trip() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.last429Time = time.Now()
	slog.Warn("console circuit breaker: 429 detected, will retry with random delay")
}

// tripFromRetryAfter records 429 with retry-after info for logging.
func (cb *consoleTeamCircuitBreaker) tripFromRetryAfter(retryAfterSec int) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.last429Time = time.Now()
	slog.Warn("console circuit breaker: 429 detected", "retry_after_sec", retryAfterSec)
}

// isConsoleRateLimitError checks if the error is a 429 rate limit error.
func isConsoleRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "429") || strings.Contains(msg, "rate_limit") || strings.Contains(msg, "resource-exhausted")
}

// Global console circuit breaker - tracking only, no blocking
var consoleCircuitBreaker = newConsoleTeamCircuitBreaker(60)
