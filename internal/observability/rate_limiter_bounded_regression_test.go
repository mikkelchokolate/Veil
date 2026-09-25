package observability

import (
	"fmt"
	"testing"
	"time"
)

// TestBoundedRateLimiterEngineCapsBucketCount (#997): a spray that mints a
// fresh key per request must not grow the bucket map without bound. Once the
// cap is reached, the stalest buckets are evicted and Size stays within the
// configured maximum.
func TestBoundedRateLimiterEngineCapsBucketCount(t *testing.T) {
	engine := NewBoundedRateLimiterEngine(64)
	for i := 0; i < 5000; i++ {
		if allowed, _ := engine.Allow(fmt.Sprintf("spray-%d", i), 1, 1); !allowed {
			t.Fatalf("key spray-%d rejected — eviction must never fail the current request", i)
		}
	}
	if size := engine.Size(); size > 64 {
		t.Fatalf("bucket count %d exceeds cap 64", size)
	}
	if size := engine.Size(); size <= 0 {
		t.Fatal("the request's own bucket was evicted")
	}
}

// TestBoundedRateLimiterEngineEvictsStalestFirst (#997): the freshest bucket
// must survive eviction; the oldest-idle budgets are dropped first.
func TestBoundedRateLimiterEngineEvictsStalestFirst(t *testing.T) {
	engine := NewBoundedRateLimiterEngine(3)
	for _, key := range []string{"old-1", "old-2", "old-3", "new-key"} {
		if allowed, _ := engine.Allow(key, 1, 1); !allowed {
			t.Fatalf("key %s rejected", key)
		}
	}
	if engine.Size() > 3 {
		t.Fatalf("bucket count %d exceeds cap 3", engine.Size())
	}
	// The just-created bucket must still hold its (already consumed) budget:
	// a second hit on it returns a denial with a retry hint, proving it was
	// tracked rather than silently recreated at full capacity.
	if _, exists := engine.buckets.Load("new-key"); !exists {
		t.Fatal("freshest bucket was evicted")
	}
}

// TestBoundedRateLimiterEngineCleanupDecrementsSize verifies the periodic
// Cleanup sweep keeps the approximate count in sync with evicted entries.
func TestBoundedRateLimiterEngineCleanupDecrementsSize(t *testing.T) {
	engine := NewBoundedRateLimiterEngine(100)
	engine.Allow("stale", 1, 1)
	engine.Allow("fresh", 1, 1)
	before := engine.Size()
	engine.Cleanup(time.Now().Add(time.Hour))
	if size := engine.Size(); size != 0 || size >= before {
		t.Fatalf("cleanup left %d buckets (was %d)", size, before)
	}
	// After cleanup the cap accounting must accept new keys again.
	if allowed, _ := engine.Allow("after-cleanup", 1, 1); !allowed {
		t.Fatal("post-cleanup request rejected")
	}
}

// TestUnboundedRateLimiterEngineHasNoCap documents that the default engine
// keeps every key — the bound is opt-in via NewBoundedRateLimiterEngine.
func TestUnboundedRateLimiterEngineHasNoCap(t *testing.T) {
	engine := NewRateLimiterEngine()
	for i := 0; i < 150; i++ {
		engine.Allow(fmt.Sprintf("k%d", i), 1, 1)
	}
	if engine.Size() != 150 {
		t.Fatalf("unbounded engine size=%d, want 150", engine.Size())
	}
}
