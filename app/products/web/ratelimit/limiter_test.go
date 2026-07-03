package ratelimit

import (
	"testing"
	"time"
)

func TestLimiterBlocksAfterThreshold(t *testing.T) {
	l := New(3, time.Minute)
	if !l.Allow("user1") {
		t.Fatal("key should be allowed initially")
	}
	for i := 0; i < 2; i++ {
		l.Fail("user1")
		if !l.Allow("user1") {
			t.Fatalf("key should still be allowed after %d failures", i+1)
		}
	}
	l.Fail("user1")
	if l.Allow("user1") {
		t.Fatal("key should be blocked after 3 failures")
	}
}

func TestLimiterSuccessClearsHistory(t *testing.T) {
	l := New(2, time.Minute)
	l.Fail("user1")
	l.Fail("user1")
	if l.Allow("user1") {
		t.Fatal("key should be blocked after 2 failures")
	}
	l.Success("user1")
	if !l.Allow("user1") {
		t.Fatal("key should be allowed after success")
	}
}

func TestLimiterUsesDefaultsWithoutPanics(t *testing.T) {
	l := New(0, 0)
	if !l.Allow("k") {
		t.Fatal("new limiter should allow")
	}
	l.Fail("k")
	if !l.Allow("k") {
		t.Fatal("should not block on first failure")
	}
}
