package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// #337/#583/#594: GET on public subscriptions and sensitive credential/export
// reads must hit a dedicated limit; cheap reads and mutations on shared
// prefixes stay on their own budgets. /api/client-links and /api/backups/
// carry dedicated all-method limits in DefaultRateLimitPolicy, so the
// GET/HEAD-only map only needs the per-resource client link/token reads.
func TestSensitiveReadPathsUseDedicatedLimits(t *testing.T) {
	for _, path := range []string{
		"/s/abc123",
		"/api/client-links",
		"/api/v1/clients/c-1/links",
		"/api/v1/clients/c-1/tokens/t-1",
		"/api/backups/veil_backup_1.tar.gz.enc/download",
		"/api/logs",
		"/api/v1/events",
		"/api/v1/traffic/stream",
	} {
		if !isRateLimitedReadPath(path) {
			t.Errorf("GET %s must select the read-path rate limit", path)
		}
	}
	for _, path := range []string{
		"/api/v1/clients",
		"/api/v1/clients/c-1",
		"/api/backups",
		"/api/settings",
		"/api/status",
		"/metrics",
	} {
		if isRateLimitedReadPath(path) {
			t.Errorf("%s unexpectedly on read-path limit", path)
		}
	}
}

func TestSensitiveReadRequestsGetThrottled(t *testing.T) {
	policy := RateLimitPolicy{
		DefaultRatePerMinute: 6000,
		DefaultBurst:         100,
		readLimits: map[string]EndpointLimit{
			"/api/v1/clients": {RatePerMinute: 60, Burst: 1},
		},
		limits: map[string]EndpointLimit{
			"/s/":               {RatePerMinute: 30, Burst: 1},
			"/api/client-links": {RatePerMinute: 30, Burst: 1},
			"/api/backups/":     {RatePerMinute: 30, Burst: 1},
		},
	}
	limiter := policy.NewLimiter()
	defer limiter.Stop()
	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/client-links"},
		{http.MethodGet, "/api/v1/clients/c-1/tokens/t-1"},
		{http.MethodGet, "/api/backups/b.tar.gz.enc/download"},
		{http.MethodGet, "/s/tok"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.RemoteAddr = "203.0.113.7:443"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s first request: %d, want 200", tc.method, tc.path, rec.Code)
		}
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("%s %s second request: %d, want 429", tc.method, tc.path, rec.Code)
		}
	}

	// Mutations on the same prefixes are not read-path limited: they keep the
	// shared default budget (100 burst here), so a PATCH sails through.
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/clients/abc", nil)
		req.RemoteAddr = "203.0.113.9:443"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("PATCH clients unexpectedly limited: %d", rec.Code)
		}
	}
}
