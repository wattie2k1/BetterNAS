package auth

import (
	"sync"
	"time"
)

// RateLimiter zählt fehlgeschlagene Anmeldungen je Client-IP in einem Zeitfenster.
type RateLimiter struct {
	mu       sync.Mutex
	max      int
	window   time.Duration
	now      func() time.Time
	failures map[string][]time.Time
}

// NewRateLimiter erlaubt max Fehlversuche je IP innerhalb von window.
func NewRateLimiter(max int, window time.Duration) *RateLimiter {
	return &RateLimiter{max: max, window: window, now: time.Now, failures: map[string][]time.Time{}}
}

func (r *RateLimiter) prune(ip string) []time.Time {
	cut := r.now().Add(-r.window)
	kept := r.failures[ip][:0]
	for _, t := range r.failures[ip] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(r.failures, ip)
		return nil
	}
	r.failures[ip] = kept
	return kept
}

// Blocked meldet, ob die IP gesperrt ist.
func (r *RateLimiter) Blocked(ip string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.prune(ip)) >= r.max
}

// Fail zählt einen Fehlversuch.
func (r *RateLimiter) Fail(ip string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prune(ip)
	r.failures[ip] = append(r.failures[ip], r.now())
}

// Sweep entfernt alle IPs ohne Fehlversuch im Zeitfenster (sonst nur beim nächsten Zugriff der IP).
func (r *RateLimiter) Sweep() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for ip := range r.failures {
		r.prune(ip)
	}
}
