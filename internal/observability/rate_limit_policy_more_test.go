package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRateLimitPolicyNewLimiterConfiguresDefaultsAndEndpoints(t *testing.T) {
	policy := DefaultRateLimitPolicy()
	limiter := policy.NewLimiter()
	defer limiter.Stop()

	if limiter.burst != policy.DefaultBurst {
		t.Fatalf("burst = %d, want %d", limiter.burst, policy.DefaultBurst)
	}
	wantRate := float64(policy.DefaultRatePerMinute) / 60.0
	if limiter.rate != wantRate {
		t.Fatalf("rate = %f, want %f", limiter.rate, wantRate)
	}

	limiter.mu.RLock()
	defer limiter.mu.RUnlock()
	for path := range policy.limits {
		if _, ok := limiter.endpointLimits[path]; !ok {
			t.Fatalf("endpoint limit missing for %s", path)
		}
	}
}

func TestRateLimitPolicyNewLimiterRateLimits(t *testing.T) {
	policy := RateLimitPolicy{
		DefaultRatePerMinute: 60,
		DefaultBurst:         1,
		limits: map[string]EndpointLimit{
			"/api/tools/speedtest": {RatePerMinute: 2, Burst: 1},
		},
	}
	limiter := policy.NewLimiter()
	defer limiter.Stop()

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/settings", nil)
	req.RemoteAddr = "192.0.2.1:12345"

	// First request should pass.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rec.Code)
	}

	// Immediately retry with the same IP should be limited.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: expected 429, got %d", rec.Code)
	}
}
