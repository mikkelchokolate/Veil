package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

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

func TestRateLimitIPFromXForwardedFor(t *testing.T) {
	limiter := NewRateLimiter(60, 1)
	t.Cleanup(func() { limiter.Stop() })

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Set X-Forwarded-For to a specific IP
	req1 := httptest.NewRequest(http.MethodPost, "/api/settings", nil)
	req1.Header.Set("X-Forwarded-For", "203.0.113.5")
	req1.RemoteAddr = "10.0.0.1:9999" // proxy address
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first request via proxy: expected 200, got %d", w1.Code)
	}

	// Second request with same X-Forwarded-For IP should be limited
	req2 := httptest.NewRequest(http.MethodPost, "/api/settings", nil)
	req2.Header.Set("X-Forwarded-For", "203.0.113.5")
	req2.RemoteAddr = "10.0.0.2:9999" // different proxy address
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request via proxy: expected 429, got %d", w2.Code)
	}

	// Different X-Forwarded-For IP should be allowed
	req3 := httptest.NewRequest(http.MethodPost, "/api/settings", nil)
	req3.Header.Set("X-Forwarded-For", "203.0.113.6")
	req3.RemoteAddr = "10.0.0.1:9999"
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("different client IP: expected 200, got %d", w3.Code)
	}
}

func TestRateLimitIPFromXForwardedForChain(t *testing.T) {
	limiter := NewRateLimiter(60, 1)
	t.Cleanup(func() { limiter.Stop() })

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// X-Forwarded-For chain: client is the first IP
	req1 := httptest.NewRequest(http.MethodPost, "/api/settings", nil)
	req1.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1, 10.0.0.2")
	req1.RemoteAddr = "10.0.0.3:9999"
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first request with chain: expected 200, got %d", w1.Code)
	}

	// Second request with same client IP (first in chain) should be limited
	req2 := httptest.NewRequest(http.MethodPost, "/api/settings", nil)
	req2.Header.Set("X-Forwarded-For", "203.0.113.7")
	req2.RemoteAddr = "10.0.0.4:9999"
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request with same client: expected 429, got %d", w2.Code)
	}
}

func TestRateLimitIPFromRemoteAddr(t *testing.T) {
	limiter := NewRateLimiter(60, 1)
	t.Cleanup(func() { limiter.Stop() })

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// RemoteAddr with port
	req1 := httptest.NewRequest(http.MethodPost, "/api/settings", nil)
	req1.RemoteAddr = "192.0.2.42:54321"
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", w1.Code)
	}

	// Same IP, different port -> same bucket -> limited
	req2 := httptest.NewRequest(http.MethodPost, "/api/settings", nil)
	req2.RemoteAddr = "192.0.2.42:12345"
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("same IP different port: expected 429, got %d", w2.Code)
	}
}

func TestRateLimitIPFromRemoteAddrNoPort(t *testing.T) {
	limiter := NewRateLimiter(60, 1)
	t.Cleanup(func() { limiter.Stop() })

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// RemoteAddr without port (unusual but valid)
	req1 := httptest.NewRequest(http.MethodPost, "/api/settings", nil)
	req1.RemoteAddr = "192.0.2.99"
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first request no-port: expected 200, got %d", w1.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/settings", nil)
	req2.RemoteAddr = "192.0.2.99"
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request no-port: expected 429, got %d", w2.Code)
	}
}

func TestRateLimitDefaultLimits(t *testing.T) {
	limiter := NewRateLimiter(100, 20)
	t.Cleanup(func() { limiter.Stop() })

	if limiter.rate <= 0 {
		t.Fatal("expected positive default rate")
	}
	if limiter.burst != 20 {
		t.Fatalf("expected burst 20, got %d", limiter.burst)
	}
	// 100 req/min = 1.666... tokens/sec
	expectedRate := 100.0 / 60.0
	if limiter.rate != expectedRate {
		t.Fatalf("expected rate %f, got %f", expectedRate, limiter.rate)
	}
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

func TestRateLimitEndpointLongestPrefixMatch(t *testing.T) {
	limiter := NewRateLimiter(60, 1)
	limiter.SetEndpointLimits(map[string]EndpointLimit{
		"/api/tools":             {RatePerMinute: 120, Burst: 3},
		"/api/tools/speedtest":   {RatePerMinute: 2, Burst: 1},
	})
	t.Cleanup(func() { limiter.Stop() })

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// /api/tools/speedtest should match the longer prefix with stricter limit
	req := httptest.NewRequest(http.MethodPost, "/api/tools/speedtest", nil)
	req.RemoteAddr = "192.0.2.1:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first speedtest: expected 200, got %d", w.Code)
	}
	// Second should be rate limited (burst=1 for speedtest)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("second speedtest: expected 429 (stricter limit), got %d", w.Code)
	}

	// /api/tools/other should use the /api/tools limit (burst=3)
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/tools/other", nil)
		req.RemoteAddr = "192.0.2.1:12345"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("/api/tools/other request %d: expected 200, got %d", i+1, w.Code)
		}
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
