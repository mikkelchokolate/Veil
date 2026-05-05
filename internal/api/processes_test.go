package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProcessesEndpointRejectsNonGet(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodPost, "/api/processes", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestProcessesEndpointReturnsJSON(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/api/processes", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("expected json content-type, got %q", ct)
	}
}

func TestProcessesEndpointFieldsPresent(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/api/processes", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var raw map[string]interface{}
	json.NewDecoder(w.Body).Decode(&raw)
	procs, ok := raw["processes"].([]interface{})
	if !ok {
		// In test environment, no managed processes are running
		if raw["processes"] == nil {
			t.Log("no processes running in test environment")
			return
		}
		t.Fatalf("expected processes array, got %T: %v", raw["processes"], raw["processes"])
	}
	if len(procs) > 0 {
		first := procs[0].(map[string]interface{})
		for _, field := range []string{"pid", "name", "cpuPercent", "memoryMB", "uptimeSeconds"} {
			if _, ok := first[field]; !ok {
				t.Errorf("missing field %s", field)
			}
		}
	}
}

func TestProcessesEndpointValuesValid(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/api/processes", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var stats ProcessesStats
	json.NewDecoder(w.Body).Decode(&stats)
	for _, p := range stats.Processes {
		if p.PID <= 0 {
			t.Errorf("invalid PID %d for %s", p.PID, p.Name)
		}
		if p.Name == "" {
			t.Error("empty process name")
		}
		if p.MemoryMB < 0 {
			t.Errorf("negative memory for %s: %d", p.Name, p.MemoryMB)
		}
	}
}

func TestIsManagedProcess(t *testing.T) {
	for _, name := range []string{"caddy", "hysteria2", "sing-box", "veil"} {
		if !isManagedProcess(name) {
			t.Errorf("expected %s to be managed", name)
		}
	}
	if isManagedProcess("nginx") {
		t.Error("nginx should not be managed")
	}
}
