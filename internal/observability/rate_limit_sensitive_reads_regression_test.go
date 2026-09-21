package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// #337/#583/#594: GET on public subscriptions and sensitive credential/export
// reads must hit a dedicated limit; cheap reads and mutations on shared
// prefixes stay on their own budgets. /api/client-links and /api/backups/
// carry dedicated all-method limits in DefaultRateLimitPolicy, so the
// GET/HEAD-only map only needs the per-resource client link/token reads.
// #617/#619/#641/#645/#648 extend the same gate to the admin WARP credential
// read, the token list (which embeds every recoverable subscription URL), and
// the expensive host diagnostic scans.
func TestSensitiveReadPathsUseDedicatedLimits(t *testing.T) {
	for _, path := range []string{
		"/s/abc123",
		"/api/client-links",
		"/api/v1/clients/c-1/links",
		"/api/v1/clients/c-1/tokens",
		"/api/v1/clients/c-1/tokens/t-1",
		"/api/backups/veil_backup_1.tar.gz.enc/download",
		"/api/logs",
		"/api/warp",
		"/api/disk",
		"/api/connections",
		"/api/runtime/observation",
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
		// /api/settings is polled by the UI and stays off the read-path list
		// deliberately (its GET response redacts secrets for viewers).
		"/api/settings",
		"/api/processes",
		"/api/system",
		"/api/network",
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
			"/api/v1/clients":          {RatePerMinute: 60, Burst: 1},
			"/api/warp":                {RatePerMinute: 60, Burst: 1},
			"/api/disk":                {RatePerMinute: 60, Burst: 1},
			"/api/connections":         {RatePerMinute: 60, Burst: 1},
			"/api/runtime/observation": {RatePerMinute: 60, Burst: 1},
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

	// Each row uses its own client IP so prefix-keyed buckets stay isolated;
	// burst is 1 for every configured read limit above.
	for _, tc := range []struct{ method, path, ip string }{
		{http.MethodGet, "/api/client-links", "203.0.113.7"},
		{http.MethodGet, "/api/v1/clients/c-1/tokens", "203.0.113.8"},
		{http.MethodGet, "/api/v1/clients/c-1/tokens/t-1", "203.0.113.10"},
		{http.MethodGet, "/api/backups/b.tar.gz.enc/download", "203.0.113.11"},
		{http.MethodGet, "/api/warp", "203.0.113.12"},
		{http.MethodGet, "/api/disk", "203.0.113.13"},
		{http.MethodGet, "/api/connections", "203.0.113.14"},
		{http.MethodGet, "/api/runtime/observation", "203.0.113.15"},
		{http.MethodGet, "/s/tok", "203.0.113.16"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.RemoteAddr = tc.ip + ":443"
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

	// #619: the token list drains the same /api/v1/clients read bucket as the
	// token-by-id reveal, so the list cannot be used to bypass that throttle.
	sharedBucketReq := httptest.NewRequest(http.MethodGet, "/api/v1/clients/c-1/tokens/t-1", nil)
	sharedBucketReq.RemoteAddr = "203.0.113.8:443"
	sharedBucketRec := httptest.NewRecorder()
	handler.ServeHTTP(sharedBucketRec, sharedBucketReq)
	if sharedBucketRec.Code != http.StatusTooManyRequests {
		t.Fatalf("token-by-id after list drained the clients read bucket: %d, want 429", sharedBucketRec.Code)
	}

	// HEAD shares the read-method gate: the observation bucket above is
	// already exhausted for this IP, so a HEAD is throttled too.
	headReq := httptest.NewRequest(http.MethodHead, "/api/runtime/observation", nil)
	headReq.RemoteAddr = "203.0.113.15:443"
	headRec := httptest.NewRecorder()
	handler.ServeHTTP(headRec, headReq)
	if headRec.Code != http.StatusTooManyRequests {
		t.Fatalf("HEAD /api/runtime/observation after exhausted read bucket: %d, want 429", headRec.Code)
	}

	// Mutations on the same prefixes are not read-path limited: they keep the
	// shared default budget (100 burst here), so a PATCH sails through. The
	// same applies to PUT /api/warp, whose read budget above is GET/HEAD-only.
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/clients/abc", nil)
		req.RemoteAddr = "203.0.113.9:443"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("PATCH clients unexpectedly limited: %d", rec.Code)
		}
	}
	putReq := httptest.NewRequest(http.MethodPut, "/api/warp", nil)
	putReq.RemoteAddr = "203.0.113.12:443" // same IP whose GET /api/warp bucket is exhausted above
	putRec := httptest.NewRecorder()
	handler.ServeHTTP(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("PUT /api/warp after exhausted GET bucket: %d, want 200 (mutations use the default budget)", putRec.Code)
	}
}

// The production DefaultRateLimitPolicy must actually throttle the batch-O
// read paths (#617/#619/#641/#645/#648): each endpoint's own burst is
// exhausted and the next GET returns 429 with Retry-After.
func TestDefaultPolicyThrottlesCredentialAndDiagnosticReads(t *testing.T) {
	limiter := DefaultRateLimitPolicy().NewLimiter()
	t.Cleanup(func() { limiter.Stop() })
	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Distinct IPs per row keep the prefix-keyed buckets isolated; the
	// /api/v1/clients subtree shares one 60/min burst-12 read bucket across
	// list, reveal, and links by design (#583/#594/#619).
	for _, tc := range []struct {
		path  string
		ip    string
		burst int
	}{
		{"/api/warp", "203.0.113.20", 3},
		{"/api/disk", "203.0.113.21", 2},
		{"/api/connections", "203.0.113.22", 2},
		{"/api/runtime/observation", "203.0.113.23", 1},
		{"/api/v1/clients/c-1/tokens", "203.0.113.24", 12},
		{"/api/v1/clients/c-1/tokens/t-1", "203.0.113.25", 12},
		{"/api/v1/clients/c-1/links", "203.0.113.26", 12},
	} {
		for i := 0; i < tc.burst; i++ {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.RemoteAddr = tc.ip + ":443"
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s request %d: %d, want 200 (burst %d)", tc.path, i+1, rec.Code, tc.burst)
			}
		}
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req.RemoteAddr = tc.ip + ":443"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("GET %s request %d: %d, want 429", tc.path, tc.burst+1, rec.Code)
		}
		if rec.Header().Get("Retry-After") == "" {
			t.Fatalf("GET %s 429 missing Retry-After", tc.path)
		}
	}
}

// Read-path buckets are token buckets: after the burst is spent the bucket
// refills at the configured rate and requests flow again (#641/#645/#648).
func TestReadPathLimitRefillsAfterThrottle(t *testing.T) {
	policy := RateLimitPolicy{
		DefaultRatePerMinute: 6000,
		DefaultBurst:         100,
		readLimits: map[string]EndpointLimit{
			"/api/disk": {RatePerMinute: 600, Burst: 1}, // 10 tokens/sec
		},
	}
	limiter := policy.NewLimiter()
	defer limiter.Stop()
	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/disk", nil)
	req.RemoteAddr = "203.0.113.30:443"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first GET /api/disk: %d, want 200", rec.Code)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second GET /api/disk: %d, want 429", rec.Code)
	}

	// ~100ms refills one token at 10 tokens/sec.
	time.Sleep(150 * time.Millisecond)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/disk after refill: %d, want 200", rec.Code)
	}
}
