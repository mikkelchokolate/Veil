package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

var _rateLimitTestDeps_engine = []any{
	http.MethodGet, httptest.NewRecorder, testing.T{}, time.Second,
}

func TestRateLimitTokenRefill(t *testing.T) {
	limiter := NewRateLimiter(600, 1) // 600 req/min = 10 req/sec, burst 1
	t.Cleanup(func() { limiter.Stop() })

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Use the single token
	req := httptest.NewRequest(http.MethodPost, "/api/settings", nil)
	req.RemoteAddr = "192.0.2.1:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", w.Code)
	}

	// Immediately should fail
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("immediate retry: expected 429, got %d", w.Code)
	}

	// Wait for token refill (~100ms is enough with 10 tokens/sec)
	time.Sleep(150 * time.Millisecond)

	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("after refill: expected 200, got %d (tokens should have refilled)", w.Code)
	}
}

func TestRateLimitCleanupRemovesStaleBuckets(t *testing.T) {
	rl := NewRateLimiter(60, 5)
	t.Cleanup(func() { rl.Stop() })

	// Insert a recent bucket
	rl.buckets.Store("recent", &tokenBucket{
		tokens:   5,
		lastTime: time.Now(),
	})

	// Insert a stale bucket (older than 10 minutes)
	rl.buckets.Store("stale", &tokenBucket{
		tokens:   0,
		lastTime: time.Now().Add(-11 * time.Minute),
	})

	// Insert a borderline bucket (9 minutes old, within the 10 min cutoff)
	rl.buckets.Store("borderline", &tokenBucket{
		tokens:   3,
		lastTime: time.Now().Add(-9 * time.Minute),
	})

	// Run cleanup
	rl.cleanup()

	// "recent" should still exist
	if _, exists := rl.buckets.Load("recent"); !exists {
		t.Fatal("expected 'recent' bucket to still exist after cleanup")
	}

	// "borderline" should still exist (9 min < 10 min cutoff)
	if _, exists := rl.buckets.Load("borderline"); !exists {
		t.Fatal("expected 'borderline' bucket (9 min old) to still exist after cleanup")
	}

	// "stale" should have been deleted (11 min > 10 min cutoff)
	if _, exists := rl.buckets.Load("stale"); exists {
		t.Fatal("expected 'stale' bucket (11 min old) to be deleted by cleanup")
	}
}
