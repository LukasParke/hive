package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestAllowBurstThenRefill verifies the fixed-window behavior: a burst up to
// the limit is allowed, the next request is denied, and a fresh window
// admits requests again.
func TestAllowBurstThenRefill(t *testing.T) {
	rl := NewRateLimiter(3, 40*time.Millisecond)

	for i := range 3 {
		if !rl.Allow("10.0.0.1") {
			t.Fatalf("request %d in burst denied", i+1)
		}
	}
	if rl.Allow("10.0.0.1") {
		t.Fatal("request over burst limit allowed")
	}
	if rl.Allow("10.0.0.1") {
		t.Fatal("second request over burst limit allowed")
	}

	// A different IP is unaffected (per-IP isolation).
	if !rl.Allow("10.0.0.2") {
		t.Fatal("second IP denied despite first IP exhaustion")
	}

	time.Sleep(60 * time.Millisecond)
	if !rl.Allow("10.0.0.1") {
		t.Fatal("request after window expiry denied")
	}
}

// TestAllowConcurrentAccess hammers Allow from many goroutines to shake out
func TestAllowConcurrentAccess(t *testing.T) {
	rl := NewRateLimiter(1000, time.Second)
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				rl.Allow("10.5.5.5")
			}
		}()
	}
	wg.Wait()
}

// TestHandlerLimitsByRemoteIP drives the HTTP middleware with fast requests
// from one IP and asserts the 429 envelope plus per-IP isolation.
func TestHandlerLimitsByRemoteIP(t *testing.T) {
	rl := NewRateLimiter(2, time.Second)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := rl.Handler(inner)

	do := func(remoteIP string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/ping", nil)
		req.RemoteAddr = remoteIP + ":54321"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	for range 2 {
		if rec := do("10.9.9.1"); rec.Code != http.StatusOK {
			t.Fatalf("in-burst request status = %d, want 200", rec.Code)
		}
	}
	rec := do("10.9.9.1")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("third fast request status = %d, want 429", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "rate limit exceeded") {
		t.Fatalf("429 body = %s, want rate limit message", rec.Body.String())
	}
	if rec := do("10.9.9.2"); rec.Code != http.StatusOK {
		t.Fatalf("isolated IP status = %d, want 200", rec.Code)
	}
}

// TestHandlerRemoteAddrWithoutPort covers the SplitHostPort error branch.
func TestHandlerRemoteAddrWithoutPort(t *testing.T) {
	rl := NewRateLimiter(1, time.Second)
	h := rl.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.RemoteAddr = "10.1.1.1" // no port
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first no-port request status = %d, want 200", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second no-port request status = %d, want 429", rec.Code)
	}
}

// TestHandlerContextCancellation ensures the middleware honors the request
// context contract by passing it through untouched.
func TestHandlerContextCancellation(t *testing.T) {
	rl := NewRateLimiter(1, time.Second)
	seen := make(chan context.Context, 1)
	h := rl.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Context()
	}))
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)
	select {
	case ctx := <-seen:
		if ctx == nil {
			t.Fatal("downstream handler saw nil context")
		}
	default:
		t.Fatal("downstream handler never ran")
	}
}
