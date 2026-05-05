package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestMetricsEndpointRequiresGET(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodPost, "/metrics", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestMetricsEndpointReturnsPrometheusFormat(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Errorf("expected text/plain content-type, got %q", ct)
	}
	body := w.Body.String()
	for _, want := range []string{
		"# HELP veil_uptime_seconds",
		"# TYPE veil_uptime_seconds gauge",
		"veil_uptime_seconds",
		"# HELP veil_http_requests_total",
		"# TYPE veil_http_requests_total counter",
		"# HELP veil_http_requests_active",
		"# HELP veil_rate_limit_hits_total",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output missing %q", want)
		}
	}
}

func TestMetricsEndpointHEADReturnsNoBody(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodHead, "/metrics", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("expected empty body for HEAD, got %d bytes", w.Body.Len())
	}
}

func TestMetricsMiddlewareTracksRequests(t *testing.T) {
	metrics := NewMetricsCollector()

	handler := metrics.MetricsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if metrics.requestsTotal.Load() != 1 {
		t.Errorf("expected 1 request, got %d", metrics.requestsTotal.Load())
	}

	// Check per-path counter
	val, ok := metrics.requestsByPath.Load("GET:/api/status")
	if !ok {
		t.Fatal("expected path counter for GET:/api/status")
	}
	if val.(*atomic.Int64).Load() != 1 {
		t.Errorf("expected path counter = 1, got %d", val.(*atomic.Int64).Load())
	}

	// Check per-code counter
	val2, ok2 := metrics.requestsByCode.Load("200")
	if !ok2 {
		t.Fatal("expected code counter for 200")
	}
	if val2.(*atomic.Int64).Load() != 1 {
		t.Errorf("expected code counter = 1, got %d", val2.(*atomic.Int64).Load())
	}
}

func TestMetricsMiddlewareTracksErrorStatusCodes(t *testing.T) {
	metrics := NewMetricsCollector()

	handler := metrics.MetricsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	val, ok := metrics.requestsByCode.Load("404")
	if !ok {
		t.Fatal("expected code counter for 404")
	}
	if val.(*atomic.Int64).Load() != 1 {
		t.Errorf("expected 404 counter = 1, got %d", val.(*atomic.Int64).Load())
	}
}

func TestMetricsMiddlewareSkipsMetricsPath(t *testing.T) {
	metrics := NewMetricsCollector()

	handler := metrics.MetricsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Request to /metrics should NOT be tracked (avoids recursion)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if metrics.requestsTotal.Load() != 0 {
		t.Errorf("expected 0 tracked requests for /metrics path, got %d", metrics.requestsTotal.Load())
	}
}

func TestMetricsTrackRateLimitHit(t *testing.T) {
	metrics := NewMetricsCollector()
	metrics.TrackRateLimitHit()
	metrics.TrackRateLimitHit()
	if metrics.rateLimitHits.Load() != 2 {
		t.Errorf("expected 2 rate limit hits, got %d", metrics.rateLimitHits.Load())
	}
}

func TestMetricsServiceStatus(t *testing.T) {
	metrics := NewMetricsCollector()
	metrics.SetServiceStatus("veil", true)
	metrics.SetServiceStatus("hysteria2", false)

	// Check metrics output includes service status
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	metrics.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, `veil_service_status{service="veil"} 1`) {
		t.Error("expected veil service status = 1")
	}
	if !strings.Contains(body, `veil_service_status{service="hysteria2"} 0`) {
		t.Error("expected hysteria2 service status = 0")
	}
}
