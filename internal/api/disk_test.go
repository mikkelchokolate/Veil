package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiskEndpointRejectsNonGet(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodPost, "/api/disk", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestDiskEndpointReturnsJSON(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/api/disk", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("expected json, got %q", ct)
	}
}

func TestDiskEndpointHasKnownPaths(t *testing.T) {
	// Create temp dirs to simulate veil paths
	dir := t.TempDir()
	etcDir := filepath.Join(dir, "etc", "veil")
	varDir := filepath.Join(dir, "var", "lib", "veil")
	os.MkdirAll(etcDir, 0o755)
	os.MkdirAll(varDir, 0o755)
	os.WriteFile(filepath.Join(etcDir, "test.conf"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(varDir, "state.json"), []byte("world"), 0o644)

	stats := dirSize(dir)
	if len(stats) == 0 {
		t.Error("expected at least one directory stat")
	}
	// Find the entry for our dir
	sizes := make(map[string]int64)
	for _, s := range stats {
		sizes[s.Path] = s.SizeBytes
	}
	// Check that etc subdirectory has positive size
	etcPath := filepath.Join(dir, "etc")
	if v, ok := sizes[etcPath]; !ok || v <= 0 {
		t.Errorf("expected positive size for %s, got %d", etcPath, v)
	}
}

func TestDiskEndpointFieldsPresent(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "f.txt"), []byte("data"), 0o644)
	stats := dirSize(dir)
	if len(stats) == 0 {
		t.Fatal("expected at least one subdirectory stat")
	}
	first := stats[0]
	if first.Path == "" {
		t.Error("expected non-empty path")
	}
	if first.SizeBytes < 0 {
		t.Errorf("expected non-negative size, got %d", first.SizeBytes)
	}
}
