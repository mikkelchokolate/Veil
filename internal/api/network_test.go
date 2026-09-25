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

func TestNetworkEndpointRejectsNonGet(t *testing.T) {
	r, _ := newTestRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodPost, "/api/network", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestNetworkEndpointReturnsJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows")
	}
	r, _ := newTestRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/api/network", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("expected json content-type, got %q", ct)
	}
}

// getNetwork issues GET /api/network and returns the decoded stats, asserting
// the 200 status and a clean decode — a 500 {"error":…} body must not decode
// into an empty-but-valid stats struct (issue #1012).
func getNetwork(t *testing.T, r http.Handler) veilruntime.NetworkStats {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/network", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var stats veilruntime.NetworkStats
	if err := json.NewDecoder(w.Body).Decode(&stats); err != nil {
		t.Fatalf("decode /api/network: %v (body: %s)", err, w.Body.String())
	}
	return stats
}

func TestNetworkEndpointHasLoopback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows")
	}
	r, _ := newTestRouter(ServerInfo{Version: "test"})
	stats := getNetwork(t, r)
	found := false
	for _, iface := range stats.Interfaces {
		if iface.Name == "lo" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected loopback interface 'lo' in network stats")
	}
}

func TestNetworkEndpointBytesPositive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows")
	}
	r, _ := newTestRouter(ServerInfo{Version: "test"})
	stats := getNetwork(t, r)
	// /proc/net/dev always reports at least the loopback interface — an empty
	// collection here is a broken response, not a quiet environment.
	if len(stats.Interfaces) == 0 {
		t.Fatal("expected non-empty interfaces — loopback is always present")
	}
	for _, iface := range stats.Interfaces {
		if iface.RxBytes < 0 || iface.TxBytes < 0 {
			t.Errorf("interface %s: rxBytes=%d txBytes=%d should be non-negative", iface.Name, iface.RxBytes, iface.TxBytes)
		}
	}
}

func TestNetworkEndpointHasPackets(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows")
	}
	r, _ := newTestRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/api/network", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var raw map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&raw); err != nil {
		t.Fatalf("decode /api/network: %v (body: %s)", err, w.Body.String())
	}
	ifaces, ok := raw["interfaces"].([]interface{})
	if !ok || len(ifaces) == 0 {
		t.Fatal("expected non-empty interfaces array")
	}
	first := ifaces[0].(map[string]interface{})
	for _, field := range []string{"rxPackets", "txPackets"} {
		if _, ok := first[field]; !ok {
			t.Errorf("missing field %s in first interface", field)
		}
	}
}
