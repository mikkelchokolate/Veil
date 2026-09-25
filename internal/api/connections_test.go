package api

import (
	"encoding/json"
	"net"
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

// bindKnownListener holds a real TCP listener for the duration of a test so
// /proc/net/tcp provably contains a LISTEN row — "has listeners" assertions on
// an empty environment would otherwise be vacuous (issue #1012).
func bindKnownListener(t *testing.T) *net.TCPListener {
	t.Helper()
	ln, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("bind fixture listener: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

func getConnections(t *testing.T, r http.Handler) veilruntime.ConnectionsStats {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/connections", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var stats veilruntime.ConnectionsStats
	if err := json.NewDecoder(w.Body).Decode(&stats); err != nil {
		t.Fatalf("decode /api/connections: %v (body: %s)", err, w.Body.String())
	}
	return stats
}

func TestConnectionsEndpointHasListeners(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows")
	}
	ln := bindKnownListener(t)
	wantPort := ln.Addr().(*net.TCPAddr).Port

	r, _ := newTestRouter(ServerInfo{Version: "test"})
	stats := getConnections(t, r)
	if len(stats.Listeners) == 0 {
		t.Fatal("expected non-empty listeners — the test bound a real listener")
	}
	found := false
	for _, l := range stats.Listeners {
		if l.Port == wantPort && l.Proto == "tcp" {
			found = true
		}
	}
	if !found {
		t.Fatalf("listeners missing the test-bound 127.0.0.1:%d socket: %+v", wantPort, stats.Listeners)
	}
}

func TestConnectionsEndpointFieldsPresent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows")
	}
	bindKnownListener(t)
	r, _ := newTestRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/api/connections", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var raw map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&raw); err != nil {
		t.Fatalf("decode /api/connections: %v (body: %s)", err, w.Body.String())
	}
	listeners, ok := raw["listeners"].([]interface{})
	if !ok {
		t.Fatalf("expected listeners array, got %T: %v", raw["listeners"], raw["listeners"])
	}
	if len(listeners) == 0 {
		t.Fatal("expected non-empty listeners — the test bound a real listener")
	}
	for _, entry := range listeners {
		listener, ok := entry.(map[string]interface{})
		if !ok {
			t.Fatalf("listener entry is %T, want object: %v", entry, entry)
		}
		for _, field := range []string{"proto", "address", "port"} {
			if _, ok := listener[field]; !ok {
				t.Errorf("missing field %s in %v", field, listener)
			}
		}
	}
}

func TestConnectionsEndpointNoNegativePorts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows")
	}
	bindKnownListener(t)
	r, _ := newTestRouter(ServerInfo{Version: "test"})
	stats := getConnections(t, r)
	if len(stats.Listeners) == 0 {
		t.Fatal("expected non-empty listeners — the test bound a real listener")
	}
	for _, l := range stats.Listeners {
		if l.Port <= 0 || l.Port > 65535 {
			t.Errorf("invalid port %d for %s/%s", l.Port, l.Proto, l.Address)
		}
	}
}
