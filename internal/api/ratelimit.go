package api

import (
	"sync"
	"time"
)

// ipRateLimiter is a minimal per-IP sliding-window counter. It is deliberately
// in-process and does not need Redis: FrpDeck is a single-instance tool.
// Both window and max may be hot-updated via SetMax / SetWindow so the admin
// UI can change the rule-creation throttle without restarting.
type ipRateLimiter struct {
	mu     sync.Mutex
	window time.Duration
	max    int
	stamps map[string][]time.Time
}

// newIPRateLimiter configures the per-key limit. A 0 max disables limiting.
func newIPRateLimiter(max int, window time.Duration) *ipRateLimiter {
	return &ipRateLimiter{
		window: window,
		max:    max,
		stamps: make(map[string][]time.Time),
	}
}

// SetMax atomically updates the per-window quota. Called from the
// runtime.Settings hook when the operator changes the value in the UI.
func (l *ipRateLimiter) SetMax(max int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.max = max
}

// Allow returns true when the key is still within quota and records the hit.
// Disabled limiter (max == 0) always allows.
func (l *ipRateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.max <= 0 {
		return true
	}
	now := time.Now()
	cutoff := now.Add(-l.window)

	kept := l.stamps[key][:0]
	for _, t := range l.stamps[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.stamps[key] = kept
		return false
	}
	l.stamps[key] = append(kept, now)
	return true
}
