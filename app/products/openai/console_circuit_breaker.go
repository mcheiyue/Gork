package openai

import (
	"sync"
	"time"
)

// consoleTeamCircuitBreaker prevents thundering herd when console.x.ai returns 429.
// Since the rate limit is team-level (not account-level), retrying with a different
// account is useless. Instead, we fail fast during a cooldown period.
type consoleTeamCircuitBreaker struct {
	mu            sync.Mutex
	cooldownUntil time.Time
	cooldownSec   int
}

func newConsoleTeamCircuitBreaker(cooldownSec int) *consoleTeamCircuitBreaker {
	if cooldownSec <= 0 {
		cooldownSec = 60
	}
	return &consoleTeamCircuitBreaker{
		cooldownSec: cooldownSec,
	}
}

// blocked returns true if we're still in cooldown period after a 429.
func (cb *consoleTeamCircuitBreaker) blocked() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.cooldownUntil.IsZero() {
		return false
	}
	if time.Now().Before(cb.cooldownUntil) {
		return true
	}
	// Cooldown expired, reset
	cb.cooldownUntil = time.Time{}
	return false
}

// remainingCooldown returns how long until the cooldown expires.
func (cb *consoleTeamCircuitBreaker) remainingCooldown() time.Duration {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.cooldownUntil.IsZero() {
		return 0
	}
	remaining := time.Until(cb.cooldownUntil)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// trip sets the cooldown timer after a 429 response.
func (cb *consoleTeamCircuitBreaker) trip() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	// Only extend cooldown, never shorten it
	newCooldown := time.Now().Add(time.Duration(cb.cooldownSec) * time.Second)
	if newCooldown.After(cb.cooldownUntil) {
		cb.cooldownUntil = newCooldown
	}
}

// tripFromRetryAfter sets cooldown based on Retry-After header value (seconds).
func (cb *consoleTeamCircuitBreaker) tripFromRetryAfter(retryAfterSec int) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cooldown := time.Duration(cb.cooldownSec) * time.Second
	if retryAfterSec > 0 {
		cooldown = time.Duration(retryAfterSec) * time.Second
	}
	newCooldown := time.Now().Add(cooldown)
	if newCooldown.After(cb.cooldownUntil) {
		cb.cooldownUntil = newCooldown
	}
}

// Global console circuit breaker - 60 second cooldown by default
var consoleCircuitBreaker = newConsoleTeamCircuitBreaker(60)
