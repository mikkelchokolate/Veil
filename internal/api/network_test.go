package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNetworkEndpointRejectsNonGet(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodPost, "/api/network", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestNetworkEndpointReturnsJSON(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
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

func TestNetworkEndpointHasLoopback(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/api/network", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var stats NetworkStats
	if err := json.NewDecoder(w.Body).Decode(&stats); err != nil {
		t.Fatalf("decode: %v", err)
	}
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
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/api/network", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var stats NetworkStats
	json.NewDecoder(w.Body).Decode(&stats)
	for _, iface := range stats.Interfaces {
		if iface.RxBytes < 0 || iface.TxBytes < 0 {
			t.Errorf("interface %s: rxBytes=%d txBytes=%d should be non-negative", iface.Name, iface.RxBytes, iface.TxBytes)
		}
	}
}

func TestNetworkEndpointHasPackets(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/api/network", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var raw map[string]interface{}
	json.NewDecoder(w.Body).Decode(&raw)
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
