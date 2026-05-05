package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSystemEndpointRejectsNonGet(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodPost, "/api/system", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestSystemEndpointReturnsValidJSON(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/api/system", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected json content-type, got %q", ct)
	}
}

func TestSystemEndpointCPUInRange(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/api/system", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var stats SystemStats
	if err := json.NewDecoder(w.Body).Decode(&stats); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if stats.CPUPercent < 0 || stats.CPUPercent > 100 {
		t.Errorf("cpuPercent out of range: %f", stats.CPUPercent)
	}
}

func TestSystemEndpointMemoryPositive(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/api/system", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var stats SystemStats
	json.NewDecoder(w.Body).Decode(&stats)
	if stats.MemoryTotalMB <= 0 {
		t.Errorf("memoryTotalMB should be positive: %d", stats.MemoryTotalMB)
	}
	if stats.MemoryUsedMB < 0 {
		t.Errorf("memoryUsedMB should be non-negative: %d", stats.MemoryUsedMB)
	}
}

func TestSystemEndpointDiskPositive(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/api/system", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var stats SystemStats
	json.NewDecoder(w.Body).Decode(&stats)
	if stats.DiskTotalGB <= 0 {
		t.Errorf("diskTotalGB should be positive: %f", stats.DiskTotalGB)
	}
}

func TestSystemEndpointUptimePositive(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/api/system", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var stats SystemStats
	json.NewDecoder(w.Body).Decode(&stats)
	if stats.UptimeSeconds <= 0 {
		t.Errorf("uptimeSeconds should be positive: %d", stats.UptimeSeconds)
	}
}

func TestSystemEndpointHasAllFields(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/api/system", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var raw map[string]interface{}
	json.NewDecoder(w.Body).Decode(&raw)
	for _, field := range []string{
		"cpuPercent", "memoryUsedMB", "memoryTotalMB",
		"diskUsedGB", "diskTotalGB", "uptimeSeconds",
		"loadAvg1", "loadAvg5", "loadAvg15",
	} {
		if _, ok := raw[field]; !ok {
			t.Errorf("missing field: %s", field)
		}
	}
}
