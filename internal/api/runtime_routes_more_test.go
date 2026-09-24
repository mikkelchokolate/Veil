package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRuntimeRoutesMethodNotAllowed(t *testing.T) {
	endpoints := []string{
		"/api/system",
		"/api/tls",
		"/api/network",
		"/api/connections",
		"/api/processes",
		"/api/disk",
		"/api/runtime/observation",
	}
	for _, path := range endpoints {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, nil)
			rec := httptest.NewRecorder()
			switch path {
			case "/api/system":
				handleSystemRuntime(rec, req)
			case "/api/tls":
				handleTLSRuntime(rec, req)
			case "/api/network":
				handleNetworkRuntime(rec, req)
			case "/api/connections":
				handleConnectionsRuntime(rec, req)
			case "/api/processes":
				handleProcessesRuntime(rec, req)
			case "/api/disk":
				handleDiskRuntime(rec, req)
			case "/api/runtime/observation":
				handleRuntimeObservation(rec, req)
			}
			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("POST %s status=%d", path, rec.Code)
			}
		})
	}
}

func TestRuntimeObservationReturnsObservation(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/runtime/observation", nil)
	rec := httptest.NewRecorder()
	handleRuntimeObservation(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() == 0 {
		t.Fatal("expected non-empty observation body")
	}
}
