package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMetricsMiddlewareModuleTracksNonMetricsRequests(t *testing.T) {
	collector := NewMetricsCollector()
	wrapped := NewMetricsMiddlewareModule(collector).Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	wrapped.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/settings", nil))
	if collector.requestsTotal.Load() != 1 {
		t.Fatalf("requestsTotal = %d", collector.requestsTotal.Load())
	}
}

func TestMetricsMiddlewareModuleSkipsMetricsEndpoint(t *testing.T) {
	collector := NewMetricsCollector()
	wrapped := NewMetricsMiddlewareModule(collector).Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	wrapped.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if collector.requestsTotal.Load() != 0 {
		t.Fatalf("requestsTotal = %d", collector.requestsTotal.Load())
	}
}
