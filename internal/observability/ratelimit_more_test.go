package observability

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRateLimiterOnRateLimitedCallbackIsInvoked(t *testing.T) {
	limiter := NewRateLimiter(60, 1)
	defer limiter.Stop()

	var callbackCalls atomic.Int32
	limiter.SetOnRateLimited(func() {
		callbackCalls.Add(1)
	})

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/settings", nil)
	req.RemoteAddr = "192.0.2.1:12345"

	// First request consumes the only token.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rec.Code)
	}

	// Second request is rate-limited and should invoke the callback.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: expected 429, got %d", rec.Code)
	}
	if callbackCalls.Load() != 1 {
		t.Fatalf("callback called %d times, want 1", callbackCalls.Load())
	}
}

func TestRateLimiterCleanupLoopRemovesStaleBuckets(t *testing.T) {
	origInterval := cleanupInterval.Load()
	cleanupInterval.Store(int64(15 * time.Millisecond))
	defer func() { cleanupInterval.Store(origInterval) }()

	limiter := NewRateLimiter(60, 5)
	defer limiter.Stop()

	limiter.buckets.Store("stale", &tokenBucket{
		tokens:   0,
		lastTime: time.Now().Add(-11 * time.Minute),
	})
	limiter.buckets.Store("recent", &tokenBucket{
		tokens:   5,
		lastTime: time.Now(),
	})

	// Wait for the background cleanupLoop to tick at least once.
	time.Sleep(75 * time.Millisecond)

	if _, exists := limiter.buckets.Load("stale"); exists {
		t.Fatal("expected 'stale' bucket to be removed by background cleanup")
	}
	if _, exists := limiter.buckets.Load("recent"); !exists {
		t.Fatal("expected 'recent' bucket to still exist")
	}
}
