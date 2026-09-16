package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Issue #166: PATCH is a mutating method (CSRF and idempotency already treat it
// as one) and must share the mutation rate-limit budget.
func TestRateLimitCoversPatchRequests(t *testing.T) {
	limiter := DefaultRateLimitPolicy().NewLimiter()
	t.Cleanup(func() { limiter.Stop() })

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 20; i++ {
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/clients/abc", nil)
		req.RemoteAddr = "192.0.2.10:40000"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("PATCH %d: expected 200, got %d", i+1, w.Code)
		}
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/clients/abc", nil)
	req.RemoteAddr = "192.0.2.10:40000"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("PATCH past default burst expected 429, got %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("429 response missing Retry-After")
	}
}

// Issue #155: the dedicated 12/min burst-4 limits configured for the panel SSE
// endpoints must actually be applied to GET stream opens.
func TestRateLimitCoversSSEStreamOpens(t *testing.T) {
	limiter := DefaultRateLimitPolicy().NewLimiter()
	t.Cleanup(func() { limiter.Stop() })

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, path := range []string{"/api/v1/events", "/api/v1/traffic/stream"} {
		for i := 0; i < 4; i++ {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.RemoteAddr = "192.0.2.20:40000"
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("%s request %d: expected 200, got %d", path, i+1, w.Code)
			}
		}
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "192.0.2.20:40000"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusTooManyRequests {
			t.Fatalf("%s request 5: expected 429, got %d", path, w.Code)
		}
		if w.Header().Get("Retry-After") == "" {
			t.Fatalf("%s 429 missing Retry-After", path)
		}
	}
}

func TestRateLimitKeepsCheapReadsUnlimited(t *testing.T) {
	limiter := DefaultRateLimitPolicy().NewLimiter()
	t.Cleanup(func() { limiter.Stop() })

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 30; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		req.RemoteAddr = "192.0.2.30:40000"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("/api/status request %d: expected 200, got %d", i+1, w.Code)
		}
	}
}
