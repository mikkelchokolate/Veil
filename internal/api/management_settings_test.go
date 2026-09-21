package api

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandleSettingsRejectsInvalidDomain(t *testing.T) {
	origAutoApply := autoApplyAfterMutation
	autoApplyAfterMutation = false
	defer func() { autoApplyAfterMutation = origAutoApply }()

	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "state.key")
	cipher := newTestCipher(t)
	if err := os.WriteFile(keyPath, cipher.KeyBytes(), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	statePath := filepath.Join(tmpDir, "state.json")
	state := newManagementState(ServerInfo{StatePath: statePath, KeyPath: keyPath, Mode: "dev"})
	mux := http.NewServeMux()
	state.register(mux)

	tests := []struct {
		name       string
		domain     string
		wantStatus int
	}{
		{"valid domain", "example.com", http.StatusOK},
		{"domain with protocol", "https://example.com", http.StatusBadRequest},
		{"domain with spaces", "example .com", http.StatusBadRequest},
		{"domain too long", strings.Repeat("a", 254), http.StatusBadRequest},
		{"empty domain OK", "", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"%s"}`, tt.domain)
			req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestHandleSettingsRejectsInvalidEmail(t *testing.T) {
	origAutoApply := autoApplyAfterMutation
	autoApplyAfterMutation = false
	defer func() { autoApplyAfterMutation = origAutoApply }()

	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "state.key")
	cipher := newTestCipher(t)
	if err := os.WriteFile(keyPath, cipher.KeyBytes(), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	statePath := filepath.Join(tmpDir, "state.json")
	state := newManagementState(ServerInfo{StatePath: statePath, KeyPath: keyPath, Mode: "dev"})
	mux := http.NewServeMux()
	state.register(mux)

	tests := []struct {
		name       string
		email      string
		wantStatus int
	}{
		{"valid email", "admin@example.com", http.StatusOK},
		{"email without @", "notanemail", http.StatusBadRequest},
		{"empty email OK", "", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"panelListen":"127.0.0.1:2096","mode":"dev","email":"%s"}`, tt.email)
			req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestHandleSettingsRejectsInvalidPanelListen(t *testing.T) {
	origAutoApply := autoApplyAfterMutation
	autoApplyAfterMutation = false
	defer func() { autoApplyAfterMutation = origAutoApply }()

	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "state.key")
	cipher := newTestCipher(t)
	if err := os.WriteFile(keyPath, cipher.KeyBytes(), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	statePath := filepath.Join(tmpDir, "state.json")
	state := newManagementState(ServerInfo{StatePath: statePath, KeyPath: keyPath, Mode: "dev"})
	mux := http.NewServeMux()
	state.register(mux)

	tests := []struct {
		name        string
		panelListen string
		wantStatus  int
	}{
		{"valid panelListen", "127.0.0.1:2096", http.StatusOK},
		{"panelListen without port", "127.0.0.1", http.StatusBadRequest},
		{"panelListen without host", ":2096", http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"panelListen":"%s","mode":"dev"}`, tt.panelListen)
			req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestHandleSettingsRejectsFallbackRootPathTraversal(t *testing.T) {
	origAutoApply := autoApplyAfterMutation
	autoApplyAfterMutation = false
	defer func() { autoApplyAfterMutation = origAutoApply }()

	tmpDir := t.TempDir()

	// Create a key file so newManagementState can load it
	keyPath := filepath.Join(tmpDir, "state.key")
	cipher := newTestCipher(t)
	if err := os.WriteFile(keyPath, cipher.KeyBytes(), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}

	statePath := filepath.Join(tmpDir, "state.json")
	state := newManagementState(ServerInfo{
		StatePath: statePath,
		KeyPath:   keyPath,
		Mode:      "dev",
	})

	mux := http.NewServeMux()
	state.register(mux)

	validBody := func(fallbackRoot string) []byte {
		return []byte(`{"panelListen":"127.0.0.1:2096","mode":"dev","fallbackRoot":"` + fallbackRoot + `"}`)
	}

	// The managed fallback tree is <etc>/www where <etc> honors VEIL_LIVE_ROOT
	// (set to an isolated root by TestMain). Legacy /var/lib/veil roots are
	// unreachable to veil-caddy — the unit runs as veil-proxy with
	// /var/lib/veil in InaccessiblePaths — so they are rejected outright
	// (issue #618).
	managedWWW := filepath.ToSlash(filepath.Join(filepath.Dir(os.Getenv("VEIL_LIVE_ROOT")), "www"))
	tests := []struct {
		name         string
		fallbackRoot string
		wantStatus   int
		checkRoot    bool // whether to expect fallbackRoot in response
	}{
		{"PUT <etc>/www → 200", managedWWW, http.StatusOK, true},
		{"PUT <etc>/www/site → 200", managedWWW + "/site", http.StatusOK, true},
		{"PUT legacy /var/lib/veil/www → 400", "/var/lib/veil/www", http.StatusBadRequest, false},
		{"PUT legacy /var/lib/veil subtree → 400", "/var/lib/veil/custom/path", http.StatusBadRequest, false},
		// Absolute paths outside the managed tree are rejected (no silent
		// containment-by-prepend), matching the strict renderer boundary
		// (audit #77 F4).
		{"PUT /etc/passwd → 400", "/etc/passwd", http.StatusBadRequest, false},
		{"PUT /var/lib/veil/../../../etc → 400", "/var/lib/veil/../../../etc", http.StatusBadRequest, false},
		{"PUT traversal attempt → 400", "/var/lib/veil/../../../../etc", http.StatusBadRequest, false},
		{"PUT /var/lib/veil → 400 (state dir itself)", "/var/lib/veil", http.StatusBadRequest, false},
		// Relative traversal must not escape the managed root after prepend.
		{"PUT ../www → 400 (relative escape)", "../www", http.StatusBadRequest, false},
		{"PUT ../www-again → 400", "../www2", http.StatusBadRequest, false},
		{"PUT empty → 200", "", http.StatusOK, false},
		{"PUT relative/path → 200", "relative/path", http.StatusOK, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(validBody(tt.fallbackRoot)))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}

			if tt.checkRoot {
				if !strings.Contains(rec.Body.String(), `"fallbackRoot"`) {
					t.Fatalf("response should contain fallbackRoot, got: %s", rec.Body.String())
				}
			}
		})
	}
}

// TestHandleSettingsDoesNotPersistFallbackRootDefault locks the product
// decision behind the #618 migration: when the client omits fallbackRoot the
// managed root resolves at render time through NaiveFallbackRoot instead of
// being frozen into state as an env-derived absolute path. Persisting
// DefaultFallbackRoot() at write time would couple state to the install
// layout and go stale under --etc-dir/VEIL_ETC_DIR drift.
func TestHandleSettingsDoesNotPersistFallbackRootDefault(t *testing.T) {
	origAutoApply := autoApplyAfterMutation
	autoApplyAfterMutation = false
	defer func() { autoApplyAfterMutation = origAutoApply }()

	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "state.key")
	cipher := newTestCipher(t)
	if err := os.WriteFile(keyPath, cipher.KeyBytes(), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	state := newManagementState(ServerInfo{
		StatePath: filepath.Join(tmpDir, "state.json"),
		KeyPath:   keyPath,
		Mode:      "dev",
	})
	mux := http.NewServeMux()
	state.register(mux)

	put := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{"panelListen":"127.0.0.1:2096","mode":"dev"}`))
	put.Header.Set("Content-Type", "application/json")
	prec := httptest.NewRecorder()
	mux.ServeHTTP(prec, put)
	if prec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d; body = %s", prec.Code, prec.Body.String())
	}

	grec := httptest.NewRecorder()
	mux.ServeHTTP(grec, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if grec.Code != http.StatusOK {
		t.Fatalf("GET status = %d; body = %s", grec.Code, grec.Body.String())
	}
	managedWWW := filepath.ToSlash(filepath.Join(filepath.Dir(os.Getenv("VEIL_LIVE_ROOT")), "www"))
	if body := grec.Body.String(); strings.Contains(body, managedWWW) {
		t.Fatalf("GET persisted env-derived fallback root %q: %s", managedWWW, body)
	}
}
