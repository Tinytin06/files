package main

// Per-client exponential backoff on /api/unlock. The cryptex keyspace is small,
// so we slow down repeated wrong guesses. A correct guess resets the counter.

import (
	"sync"
	"time"
)

type attempt struct {
	fails       int
	nextAllowed time.Time
}

type RateLimiter struct {
	mu      sync.Mutex
	clients map[string]*attempt
	base    time.Duration
	max     time.Duration
	free    int // failures allowed before backoff kicks in
}

func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		clients: make(map[string]*attempt),
		base:    500 * time.Millisecond,
		max:     30 * time.Second,
		free:    5,
	}
}

// Allowed reports whether key may attempt now, and if not, how long to wait.
func (r *RateLimiter) Allowed(key string) (bool, time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := r.clients[key]
	if a == nil {
		return true, 0
	}
	if d := time.Until(a.nextAllowed); d > 0 {
		return false, d
	}
	return true, 0
}

// Fail records a wrong guess and extends the backoff window.
func (r *RateLimiter) Fail(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := r.clients[key]
	if a == nil {
		a = &attempt{}
		r.clients[key] = a
	}
	a.fails++
	if a.fails <= r.free {
		return
	}
	wait := r.base << (a.fails - r.free - 1)
	if wait > r.max || wait <= 0 {
		wait = r.max
	}
	a.nextAllowed = time.Now().Add(wait)
}

// Reset clears the backoff after a correct guess.
func (r *RateLimiter) Reset(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.clients, key)
}
