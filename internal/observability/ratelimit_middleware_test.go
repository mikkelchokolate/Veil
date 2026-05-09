package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

var _rateLimitTestDeps_middleware = []any{
	http.MethodGet, httptest.NewRecorder, testing.T{}, time.Second,
}

func TestRateLimitAllowsRequestsUnderBurst(t *testing.T) {
	limiter := NewRateLimiter(60, 5) // 60 req/min, burst 5
	t.Cleanup(func() { limiter.Stop() })

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Burst of 5: first 5 POST requests should succeed
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/settings", nil)
		req.RemoteAddr = "192.0.2.1:12345"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d: %s", i+1, w.Code, w.Body.String())
		}
	}

	// 6th request should be rate limited
	req := httptest.NewRequest(http.MethodPost, "/api/settings", nil)
	req.RemoteAddr = "192.0.2.1:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after burst exhausted, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRateLimitReturnsRetryAfterHeader(t *testing.T) {
	limiter := NewRateLimiter(60, 1) // 60 req/min, burst 1
	t.Cleanup(func() { limiter.Stop() })

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request consumes the single token
	req1 := httptest.NewRequest(http.MethodPost, "/api/settings", nil)
	req1.RemoteAddr = "192.0.2.1:12345"
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first request expected 200, got %d", w1.Code)
	}

	// Second request should get 429 with Retry-After
	req2 := httptest.NewRequest(http.MethodPost, "/api/settings", nil)
	req2.RemoteAddr = "192.0.2.1:12345"
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", w2.Code, w2.Body.String())
	}
	retryAfter := w2.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Fatal("expected Retry-After header on 429 response")
	}
	if retryAfter == "0" {
		t.Fatalf("expected non-zero Retry-After, got %q", retryAfter)
	}
}

func TestRateLimitPerIPIsolation(t *testing.T) {
	limiter := NewRateLimiter(60, 1) // 60 req/min, burst 1
	t.Cleanup(func() { limiter.Stop() })

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Exhaust IP1's limit
	req := httptest.NewRequest(http.MethodPost, "/api/settings", nil)
	req.RemoteAddr = "192.0.2.1:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("IP1 first request: expected 200, got %d", w.Code)
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("IP1 second request: expected 429, got %d", w.Code)
	}

	// IP2 should still be allowed
	req2 := httptest.NewRequest(http.MethodPost, "/api/settings", nil)
	req2.RemoteAddr = "198.51.100.1:54321"
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("IP2: expected 200 (different bucket), got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestRateLimitNonMutatingMethodsBypass(t *testing.T) {
	limiter := NewRateLimiter(60, 1) // 60 req/min, burst 1
	t.Cleanup(func() { limiter.Stop() })

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Exhaust POST limit
	req := httptest.NewRequest(http.MethodPost, "/api/settings", nil)
	req.RemoteAddr = "192.0.2.1:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first POST: expected 200, got %d", w.Code)
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("second POST: expected 429, got %d", w.Code)
	}

	// GET should still work
	getReq := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	getReq.RemoteAddr = "192.0.2.1:12345"
	getW := httptest.NewRecorder()
	handler.ServeHTTP(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("GET after exhausted POST limit: expected 200, got %d", getW.Code)
	}

	// HEAD should still work
	headReq := httptest.NewRequest(http.MethodHead, "/api/status", nil)
	headReq.RemoteAddr = "192.0.2.1:12345"
	headW := httptest.NewRecorder()
	handler.ServeHTTP(headW, headReq)
	if headW.Code != http.StatusOK {
		t.Fatalf("HEAD after exhausted POST limit: expected 200, got %d", headW.Code)
	}

	// OPTIONS should still work
	optReq := httptest.NewRequest(http.MethodOptions, "/api/settings", nil)
	optReq.RemoteAddr = "192.0.2.1:12345"
	optW := httptest.NewRecorder()
	handler.ServeHTTP(optW, optReq)
	if optW.Code != http.StatusOK {
		t.Fatalf("OPTIONS after exhausted POST limit: expected 200, got %d", optW.Code)
	}
}

func TestRateLimitSpeedtestEndpointStricterLimit(t *testing.T) {
	limiter := NewRateLimiter(60, 5) // Default: 60 req/min, burst 5
	limiter.SetEndpointLimits(map[string]EndpointLimit{
		"/api/tools/speedtest": {RatePerMinute: 2, Burst: 1}, // 1 req/30s
	})
	t.Cleanup(func() { limiter.Stop() })

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First speedtest request should succeed
	speedReq := httptest.NewRequest(http.MethodPost, "/api/tools/speedtest", nil)
	speedReq.RemoteAddr = "192.0.2.1:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, speedReq)
	if w.Code != http.StatusOK {
		t.Fatalf("first speedtest: expected 200, got %d", w.Code)
	}

	// Second speedtest request (immediately) should be rate limited
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, speedReq)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("second speedtest: expected 429, got %d: %s", w.Code, w.Body.String())
	}
	retryAfter := w.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Fatal("expected Retry-After header on speedtest 429")
	}

	// Other mutating endpoints should still work (separate bucket)
	settingsReq := httptest.NewRequest(http.MethodPost, "/api/settings", nil)
	settingsReq.RemoteAddr = "192.0.2.1:12345"
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, settingsReq)
	if w2.Code != http.StatusOK {
		t.Fatalf("settings after speedtest exhaustion: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}

	// Speedtest also shouldn't consume from default bucket.
	// We've used 1 default token (settings above); burst=5 leaves 4 more.
	for i := 0; i < 4; i++ {
		req := httptest.NewRequest(http.MethodPut, "/api/warp", nil)
		req.RemoteAddr = "192.0.2.1:12345"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("warp request %d: expected 200, got %d", i+1, w.Code)
		}
	}
	// 5th warp should hit default limit (burst exhausted)
	req := httptest.NewRequest(http.MethodPut, "/api/warp", nil)
	req.RemoteAddr = "192.0.2.1:12345"
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("5th warp: expected 429 after default burst, got %d", w.Code)
	}
}

func TestRateLimitPUTAndDELETEAreLimited(t *testing.T) {
	limiter := NewRateLimiter(60, 1)
	t.Cleanup(func() { limiter.Stop() })

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// PUT should consume from the same bucket as POST
	putReq := httptest.NewRequest(http.MethodPut, "/api/settings", nil)
	putReq.RemoteAddr = "192.0.2.1:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, putReq)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT: expected 200, got %d", w.Code)
	}

	// POST after PUT should be limited (same bucket)
	postReq := httptest.NewRequest(http.MethodPost, "/api/inbounds", nil)
	postReq.RemoteAddr = "192.0.2.1:12345"
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, postReq)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("POST after PUT: expected 429, got %d", w.Code)
	}

	// DELETE also uses the same bucket
	// Reset with a new IP
	delReq := httptest.NewRequest(http.MethodDelete, "/api/inbounds/test", nil)
	delReq.RemoteAddr = "198.51.100.1:12345"
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, delReq)
	if w.Code != http.StatusOK {
		t.Fatalf("DELETE new IP: expected 200, got %d", w.Code)
	}
	// Second DELETE should be limited
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, delReq)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("second DELETE: expected 429, got %d", w.Code)
	}
}

func TestRateLimitAppliesToLogsReadPath(t *testing.T) {
	rl := NewRateLimiter(1, 1) // 1 req/min, burst 1

	var handlerCalled int
	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled++
		w.WriteHeader(http.StatusOK)
	}))

	// First GET /api/logs should pass
	req := httptest.NewRequest(http.MethodGet, "/api/logs?unit=veil&lines=10", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", w.Code)
	}

	// Second GET /api/logs should be rate-limited
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: expected 429, got %d", w2.Code)
	}
	if handlerCalled != 1 {
		t.Errorf("handler should have been called only once, got %d", handlerCalled)
	}
}

func TestRateLimitAppliesToDNSTools(t *testing.T) {
	rl := NewRateLimiter(100, 20)
	rl.SetEndpointLimits(map[string]EndpointLimit{
		"/api/tools/dns-lookup": {RatePerMinute: 10, Burst: 3},
		"/api/tools/ping":       {RatePerMinute: 5, Burst: 2},
	})

	var handlerCalled int
	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled++
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("dns-lookup allows burst of 3", func(t *testing.T) {
		handlerCalled = 0
		for i := 0; i < 3; i++ {
			req := httptest.NewRequest(http.MethodPost, "/api/tools/dns-lookup", nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("dns-lookup %d: expected 200, got %d", i+1, w.Code)
			}
		}
		// 4th should be limited
		req := httptest.NewRequest(http.MethodPost, "/api/tools/dns-lookup", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusTooManyRequests {
			t.Fatalf("dns-lookup 4th: expected 429, got %d", w.Code)
		}
		if handlerCalled != 3 {
			t.Errorf("expected 3 calls, got %d", handlerCalled)
		}
	})

	t.Run("ping allows burst of 2", func(t *testing.T) {
		handlerCalled = 0
		for i := 0; i < 2; i++ {
			req := httptest.NewRequest(http.MethodPost, "/api/tools/ping", nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("ping %d: expected 200, got %d", i+1, w.Code)
			}
		}
		// 3rd should be limited
		req := httptest.NewRequest(http.MethodPost, "/api/tools/ping", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusTooManyRequests {
			t.Fatalf("ping 3rd: expected 429, got %d", w.Code)
		}
		if handlerCalled != 2 {
			t.Errorf("expected 2 calls, got %d", handlerCalled)
		}
	})

	t.Run("dns-lookup and ping have separate buckets", func(t *testing.T) {
		// Fresh limiter for isolation
		rl2 := NewRateLimiter(100, 20)
		rl2.SetEndpointLimits(map[string]EndpointLimit{
			"/api/tools/dns-lookup": {RatePerMinute: 10, Burst: 3},
			"/api/tools/ping":       {RatePerMinute: 5, Burst: 2},
		})
		called := 0
		h2 := rl2.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called++
			w.WriteHeader(http.StatusOK)
		}))
		// Exhaust ping burst
		for i := 0; i < 2; i++ {
			req := httptest.NewRequest(http.MethodPost, "/api/tools/ping", nil)
			w := httptest.NewRecorder()
			h2.ServeHTTP(w, req)
		}
		// DNS lookup should still work (separate bucket)
		req := httptest.NewRequest(http.MethodPost, "/api/tools/dns-lookup", nil)
		w := httptest.NewRecorder()
		h2.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("dns-lookup after ping exhaustion: expected 200, got %d", w.Code)
		}
	})
}
