package observability

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetricsCollectorServeHTTP(t *testing.T) {
	collector := NewMetricsCollector()
	collector.TrackRequest("GET", "/api/status", 200, 0)

	tests := []struct {
		name       string
		method     string
		wantCode   int
		wantBody   string
		wantAbsent string
	}{
		{
			name:     "GET returns metrics body",
			method:   http.MethodGet,
			wantCode: http.StatusOK,
			wantBody: "veil_http_requests_total 1",
		},
		{
			name:       "HEAD returns headers without body",
			method:     http.MethodHead,
			wantCode:   http.StatusOK,
			wantAbsent: "veil_http_requests_total",
		},
		{
			name:     "POST is rejected with method not allowed",
			method:   http.MethodPost,
			wantCode: http.StatusMethodNotAllowed,
			wantBody: "method not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/metrics", nil)
			rec := httptest.NewRecorder()
			collector.ServeHTTP(rec, req)

			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantCode)
			}
			ct := rec.Header().Get("Content-Type")
			if tt.method != http.MethodPost && ct != "text/plain; version=0.0.4; charset=utf-8" {
				t.Fatalf("Content-Type = %q, want text/plain metrics type", ct)
			}
			body := rec.Body.String()
			if tt.wantBody != "" && !strings.Contains(body, tt.wantBody) {
				t.Fatalf("body missing %q:\n%s", tt.wantBody, body)
			}
			if tt.wantAbsent != "" && strings.Contains(body, tt.wantAbsent) {
				t.Fatalf("body should not contain %q:\n%s", tt.wantAbsent, body)
			}
		})
	}
}

func TestMetricsCollectorMetricsMiddlewareWrapsHandler(t *testing.T) {
	collector := NewMetricsCollector()
	handlerCalled := false
	wrapped := collector.MetricsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusCreated)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/settings", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if !handlerCalled {
		t.Fatal("wrapped handler was not called")
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if collector.requestsTotal.Load() != 1 {
		t.Fatalf("requestsTotal = %d, want 1", collector.requestsTotal.Load())
	}
}

func TestMetricsCollectorActiveRequestsReturnsPointer(t *testing.T) {
	collector := NewMetricsCollector()
	ar := collector.ActiveRequests()
	if ar == nil {
		t.Fatal("ActiveRequests() returned nil")
	}
	ar.Add(1)
	if collector.activeRequests.Load() != 1 {
		t.Fatalf("activeRequests = %d, want 1", collector.activeRequests.Load())
	}
}
