package main

import (
	"sync"
	"time"
)

// loginLimiter slows down password guessing:
//   - per client IP: after maxFails failures within window the IP is locked
//     until the oldest failure leaves the window;
//   - globally: login attempts are serialized and each failure takes one
//     second, so even many IPs (or a spoofed X-Real-IP) get at most about
//     one guess per second.
type loginLimiter struct {
	maxFails int
	window   time.Duration

	mu    sync.Mutex
	fails map[string][]time.Time

	serial sync.Mutex
}

func newLoginLimiter(maxFails int, window time.Duration) *loginLimiter {
	return &loginLimiter{maxFails: maxFails, window: window, fails: map[string][]time.Time{}}
}

func (l *loginLimiter) recent(ip string, now time.Time) []time.Time {
	var keep []time.Time
	for _, t := range l.fails[ip] {
		if now.Sub(t) < l.window {
			keep = append(keep, t)
		}
	}
	if len(keep) == 0 {
		delete(l.fails, ip)
	} else {
		l.fails[ip] = keep
	}
	return keep
}

// retryAfter returns how long ip is still locked (0 = not locked).
func (l *loginLimiter) retryAfter(ip string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	f := l.recent(ip, now)
	if len(f) < l.maxFails {
		return 0
	}
	return f[len(f)-l.maxFails].Add(l.window).Sub(now)
}

func (l *loginLimiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	// Bound memory if someone cycles through many addresses.
	if len(l.fails) > 10000 {
		for k := range l.fails {
			l.recent(k, now)
		}
	}
	l.fails[ip] = append(l.recent(ip, now), now)
}

func (l *loginLimiter) success(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, ip)
}
