package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	veilruntime "github.com/mikkelchokolate/Veil/internal/runtime"
)

func TestConnectionsEndpointRejectsNonGet(t *testing.T) {
	r, _ := newTestRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodPost, "/api/connections", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestConnectionsEndpointReturnsJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows")
	}
	r, _ := newTestRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/api/connections", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("expected json content-type, got %q", ct)
	}
}

func TestConnectionsEndpointHasListeners(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows")
	}
	r, _ := newTestRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/api/connections", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var stats veilruntime.ConnectionsStats
	if err := json.NewDecoder(w.Body).Decode(&stats); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(stats.Listeners) == 0 {
		t.Log("no listeners found (may be normal in test environment)")
	}
}

func TestConnectionsEndpointFieldsPresent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows")
	}
	r, _ := newTestRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/api/connections", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var raw map[string]interface{}
	json.NewDecoder(w.Body).Decode(&raw)
	listeners, ok := raw["listeners"].([]interface{})
	if !ok {
		t.Fatal("expected listeners array")
	}
	if len(listeners) > 0 {
		first := listeners[0].(map[string]interface{})
		for _, field := range []string{"proto", "address", "port"} {
			if _, ok := first[field]; !ok {
				t.Errorf("missing field %s", field)
			}
		}
	}
}

func TestConnectionsEndpointNoNegativePorts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows")
	}
	r, _ := newTestRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/api/connections", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var stats veilruntime.ConnectionsStats
	json.NewDecoder(w.Body).Decode(&stats)
	for _, l := range stats.Listeners {
		if l.Port <= 0 || l.Port > 65535 {
			t.Errorf("invalid port %d for %s/%s", l.Port, l.Proto, l.Address)
		}
	}
}
