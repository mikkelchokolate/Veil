package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiskEndpointRejectsNonGet(t *testing.T) {
	r, _ := newTestRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodPost, "/api/disk", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestDiskEndpointReturnsJSON(t *testing.T) {
	// Point the Veil-managed roots at fixtures with known content so the
	// response fields can be asserted exactly — a bare 200/JSON check greens
	// an empty or shapeless body (#931).
	etcDir := t.TempDir()
	varDir := t.TempDir()
	caddyDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(varDir, "state.json"), []byte("world"), 0o600); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 2048)
	if err := os.WriteFile(filepath.Join(etcDir, "test.conf"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VEIL_ETC_DIR", etcDir)
	t.Setenv("VEIL_VAR_DIR", varDir)
	t.Setenv("VEIL_CADDY_STATE_DIR", caddyDir)
	// Keep the response deterministic: unconfigured Mita dir that cannot
	// exist on any host, and no *_PATH fallback re-derivation.
	t.Setenv("VEIL_MITA_STATE_DIR", filepath.Join(t.TempDir(), "missing-mita"))
	t.Setenv("VEIL_STATE_PATH", "")
	t.Setenv("VEIL_KEY_PATH", "")
	t.Setenv("VEIL_LIVE_ROOT", "")

	r, _ := newTestRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/api/disk", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("expected json, got %q", ct)
	}
	var body struct {
		Dirs []struct {
			Path      string `json:"path"`
			SizeBytes int64  `json:"sizeBytes"`
			SizeHuman string `json:"sizeHuman"`
		} `json:"dirs"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode /api/disk body: %v (%s)", err, w.Body.String())
	}
	byPath := map[string]struct {
		SizeBytes int64
		SizeHuman string
	}{}
	for _, d := range body.Dirs {
		byPath[filepath.Clean(d.Path)] = struct {
			SizeBytes int64
			SizeHuman string
		}{d.SizeBytes, d.SizeHuman}
	}
	want := map[string]struct {
		bytes int64
		human string
	}{
		filepath.Clean(varDir):   {5, "5 B"},
		filepath.Clean(etcDir):   {2048, "2.0 KB"},
		filepath.Clean(caddyDir): {0, "0 B"},
	}
	for path, w2 := range want {
		got, ok := byPath[path]
		if !ok {
			t.Fatalf("dirs missing %s: %+v", path, body.Dirs)
		}
		if got.SizeBytes != w2.bytes || got.SizeHuman != w2.human {
			t.Fatalf("%s size = %d (%q), want %d (%q)", path, got.SizeBytes, got.SizeHuman, w2.bytes, w2.human)
		}
	}
}
