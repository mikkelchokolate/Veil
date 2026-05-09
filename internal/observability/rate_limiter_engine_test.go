package observability

import "testing"

func TestRateLimiterEngineConsumesBurstTokens(t *testing.T) {
	engine := NewRateLimiterEngine()
	if allowed, _ := engine.Allow("client", 1, 1); !allowed {
		t.Fatalf("first request should be allowed")
	}
	if allowed, retryAfter := engine.Allow("client", 1, 1); allowed || retryAfter <= 0 {
		t.Fatalf("second request allowed=%v retryAfter=%v", allowed, retryAfter)
	}
}
