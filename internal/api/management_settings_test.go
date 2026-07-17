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

	tests := []struct {
		name         string
		fallbackRoot string
		wantStatus   int
		checkRoot    bool // whether to expect fallbackRoot in response
	}{
		{"PUT /var/lib/veil/www → 200", "/var/lib/veil/www", http.StatusOK, true},
		{"PUT /var/lib/veil/custom/path → 200", "/var/lib/veil/custom/path", http.StatusOK, true},
		{"PUT /etc/passwd → 200 (normalized into /var/lib/veil)", "/etc/passwd", http.StatusOK, true},
		{"PUT /var/lib/veil/../../../etc → 200 (normalized)", "/var/lib/veil/../../../etc", http.StatusOK, true},
		{"PUT traversal attempt → 200 (contained by prepend)", "/var/lib/veil/../../../../etc", http.StatusOK, true},
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
