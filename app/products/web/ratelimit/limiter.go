package ratelimit

import (
	"sync"
	"time"
)

// Limiter tracks per-key failure counts and blocks keys that exceed
// a threshold within a cooldown window.
type Limiter struct {
	mu        sync.Mutex
	failures  map[string]failure
	threshold int
	cooldown  time.Duration
	now       func() time.Time
}

type failure struct {
	count        int
	blockedUntil time.Time
}

// New creates a Limiter that blocks a key after threshold consecutive failures
// for the given cooldown duration. Sensible defaults are applied for non-positive inputs.
func New(threshold int, cooldown time.Duration) *Limiter {
	if threshold <= 0 {
		threshold = 5
	}
	if cooldown <= 0 {
		cooldown = time.Minute
	}
	return &Limiter{
		failures:  map[string]failure{},
		threshold: threshold,
		cooldown:  cooldown,
		now:       time.Now,
	}
}

// Allow reports whether the given key is currently permitted.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	item := l.failures[key]
	if item.blockedUntil.IsZero() || !l.now().Before(item.blockedUntil) {
		return true
	}
	return false
}

// Fail records a failed attempt for the given key. When the failure count
// reaches the threshold the key is blocked for the cooldown duration.
func (l *Limiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	item := l.failures[key]
	item.count++
	if item.count >= l.threshold {
		item.blockedUntil = l.now().Add(l.cooldown)
	}
	l.failures[key] = item
}

// Success clears all failure history for the given key.
func (l *Limiter) Success(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, key)
}
