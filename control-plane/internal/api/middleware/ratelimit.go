package middleware

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// rateLimitEntry tracks requests per client.
type rateLimitEntry struct {
	count    int
	windowStart time.Time
}

// RateLimiter is a simple in-memory rate limiter.
type RateLimiter struct {
	requests   int
	window     time.Duration
	clients    map[string]*rateLimitEntry
	mu         sync.RWMutex
}

// NewRateLimiter creates a rate limiter allowing `requests` per `window` per client IP.
func NewRateLimiter(requests int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		requests: requests,
		window:   window,
		clients:  make(map[string]*rateLimitEntry),
	}
}

// Allow checks if the client identified by `key` is within the rate limit.
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	entry, exists := rl.clients[key]
	if !exists || now.Sub(entry.windowStart) > rl.window {
		rl.clients[key] = &rateLimitEntry{count: 1, windowStart: now}
		return true
	}

	if entry.count >= rl.requests {
		return false
	}

	entry.count++
	return true
}

// Handler returns HTTP middleware that rate-limits by remote IP.
func (rl *RateLimiter) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		if !rl.Allow(ip) {
			http.Error(w, `{"message":"rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
