package app

import (
	"testing"
	"time"
)

func TestLoginLimiterBlocksAfterFiveFailures(t *testing.T) {
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	limiter := newLoginLimiter()
	limiter.now = func() time.Time { return now }
	key := loginAttemptKey("192.0.2.10:53000", "arthur")

	for range loginFailureLimit {
		if limiter.isBlocked(key) {
			t.Fatal("isBlocked() = true before recording five failures")
		}
		limiter.recordFailure(key)
	}
	if !limiter.isBlocked(key) {
		t.Fatal("isBlocked() = false after five failures")
	}
}

func TestLoginLimiterExpiresAndResetsFailures(t *testing.T) {
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	limiter := newLoginLimiter()
	limiter.now = func() time.Time { return now }
	key := loginAttemptKey("192.0.2.10:53000", "arthur")

	for range loginFailureLimit {
		limiter.recordFailure(key)
	}
	now = now.Add(loginFailureWindow)
	if limiter.isBlocked(key) {
		t.Fatal("isBlocked() = true after the failure window")
	}

	limiter.recordFailure(key)
	limiter.reset(key)
	if limiter.isBlocked(key) {
		t.Fatal("isBlocked() = true after reset")
	}
}

func TestLoginAttemptKeyUsesHostAndNormalizedUsername(t *testing.T) {
	first := loginAttemptKey("192.0.2.10:53000", "Arthur")
	second := loginAttemptKey("192.0.2.10:53001", "arthur")
	if first != second {
		t.Fatalf("loginAttemptKey() = %q and %q, want equal keys", first, second)
	}
}
