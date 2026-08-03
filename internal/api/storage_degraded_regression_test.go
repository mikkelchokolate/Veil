package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCorruptProductionDatabaseStartsReadOnlyDegraded(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	if err := os.WriteFile(filepath.Join(root, "veil.db"), []byte("not-a-sqlite-database"), 0o600); err != nil {
		t.Fatal(err)
	}
	router, reloader := NewRouter(ServerInfo{
		Version:   "test",
		Mode:      "production",
		StatePath: statePath,
		KeyPath:   filepath.Join(root, "state.key"),
		ApplyRoot: filepath.Join(root, "staging"),
		LiveRoot:  filepath.Join(root, "live"),
	})
	defer func() {
		if closer, ok := reloader.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}()

	live := httptest.NewRecorder()
	router.ServeHTTP(live, httptest.NewRequest(http.MethodGet, "/livez", nil))
	if live.Code != http.StatusOK {
		t.Fatalf("liveness must remain available in storage-degraded mode: %d %s", live.Code, live.Body.String())
	}
	ready := httptest.NewRecorder()
	router.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if ready.Code != http.StatusServiceUnavailable || !strings.Contains(ready.Body.String(), `"sqlite"`) {
		t.Fatalf("readiness must expose SQLite degradation without paths: %d %s", ready.Code, ready.Body.String())
	}
	if strings.Contains(ready.Body.String(), root) {
		t.Fatalf("readiness leaked secret storage path: %s", ready.Body.String())
	}

	mutation := httptest.NewRecorder()
	router.ServeHTTP(mutation, httptest.NewRequest(http.MethodPost, "/api/apply", strings.NewReader(`{"confirm":true,"applyLive":true,"applyServices":true}`)))
	if mutation.Code != http.StatusServiceUnavailable || !strings.Contains(mutation.Body.String(), "storage_unavailable") {
		t.Fatalf("management mutation did not fail closed: %d %s", mutation.Code, mutation.Body.String())
	}
}
