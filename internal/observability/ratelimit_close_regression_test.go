package observability

import "testing"

func TestRateLimiterCloseStopsAndJoinsCleanupWorker(t *testing.T) {
	limiter := NewRateLimiter(60, 10)
	if err := limiter.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-limiter.doneCh:
	default:
		t.Fatal("cleanup worker still running after Close")
	}
	if err := limiter.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
