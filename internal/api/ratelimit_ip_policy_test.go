package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

var _rateLimitTestDeps_ip_policy = []any{
	http.MethodGet, httptest.NewRecorder, testing.T{}, time.Second,
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

func TestRateLimitEndpointLongestPrefixMatch(t *testing.T) {
	limiter := NewRateLimiter(60, 1)
	limiter.SetEndpointLimits(map[string]EndpointLimit{
		"/api/tools":           {RatePerMinute: 120, Burst: 3},
		"/api/tools/speedtest": {RatePerMinute: 2, Burst: 1},
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

func TestIsRateLimitedReadPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/api/logs", true},
		{"/api/logs?unit=caddy&lines=50", true},
		{"/api/status", false},
		{"/metrics", false},
		{"/healthz", false},
		{"/", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := isRateLimitedReadPath(tt.path); got != tt.want {
				t.Errorf("isRateLimitedReadPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
