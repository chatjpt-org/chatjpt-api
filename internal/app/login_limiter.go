package app

import (
	"net"
	"strings"
	"sync"
	"time"
)

const (
	loginFailureLimit  = 5
	loginFailureWindow = 15 * time.Minute
)

type loginLimiter struct {
	mu       sync.Mutex
	failures map[string]loginFailures
	now      func() time.Time
}

type loginFailures struct {
	firstFailure time.Time
	count        int
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{
		failures: make(map[string]loginFailures),
		now:      time.Now,
	}
}

func (l *loginLimiter) isBlocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	failures, ok := l.failures[key]
	if !ok {
		return false
	}
	if l.now().Sub(failures.firstFailure) >= loginFailureWindow {
		delete(l.failures, key)
		return false
	}
	return failures.count >= loginFailureLimit
}

func (l *loginLimiter) recordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	failures, ok := l.failures[key]
	if !ok || now.Sub(failures.firstFailure) >= loginFailureWindow {
		l.failures[key] = loginFailures{firstFailure: now, count: 1}
		return
	}
	failures.count++
	l.failures[key] = failures
}

func (l *loginLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, key)
}

func loginAttemptKey(remoteAddress, username string) string {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		host = remoteAddress
	}
	return host + "\x00" + strings.ToLower(username)
}
