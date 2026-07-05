package panelmaterial

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvContentEdgeCases(t *testing.T) {
	t.Parallel()

	basePaths := Paths{EtcDir: t.TempDir()}

	tests := []struct {
		name            string
		input           Input
		wantEmpty       bool
		wantContains    []string
		wantNotContains []string
	}{
		{
			name:      "empty auth token returns empty",
			input:     Input{Paths: basePaths, PanelAuthToken: ""},
			wantEmpty: true,
		},
		{
			name: "minimal token only",
			input: Input{
				Paths:          basePaths,
				PanelAuthToken: "tok",
			},
			wantContains: []string{"VEIL_API_TOKEN=tok\n"},
		},
		{
			name: "omits empty optional fields",
			input: Input{
				Paths:          basePaths,
				PanelAuthToken: "tok",
			},
			wantNotContains: []string{"VEIL_LISTEN", "VEIL_PANEL_ACCESS", "VEIL_DOMAIN", "VEIL_EMAIL", "VEIL_TLS_CERT", "VEIL_TLS_KEY", "VEIL_WEB_BASE_PATH"},
		},
		{
			name: "omits TLS when not enabled",
			input: Input{
				Paths:           basePaths,
				PanelAuthToken:  "tok",
				PanelTLSEnabled: false,
			},
			wantNotContains: []string{"VEIL_TLS_CERT", "VEIL_TLS_KEY"},
		},
		{
			name: "omits web base path when root slash",
			input: Input{
				Paths:          basePaths,
				PanelAuthToken: "tok",
				WebBasePath:    "/",
			},
			wantNotContains: []string{"VEIL_WEB_BASE_PATH"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := NewManagedMaterial(tt.input)
			got := m.EnvContent()
			if tt.wantEmpty && got != "" {
				t.Fatalf("expected empty EnvContent, got:\n%s", got)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Fatalf("env missing %q:\n%s", want, got)
				}
			}
			for _, notWant := range tt.wantNotContains {
				if strings.Contains(got, notWant) {
					t.Fatalf("env unexpectedly contains %q:\n%s", notWant, got)
				}
			}
		})
	}
}

func TestFilesValidationErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   Input
		wantErr string
	}{
		{
			name:    "missing etc dir",
			input:   Input{Paths: Paths{VarDir: "/var/lib/veil"}},
			wantErr: "etc dir is required",
		},
		{
			name:    "missing var dir",
			input:   Input{Paths: Paths{EtcDir: "/etc/veil"}},
			wantErr: "var dir is required",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := NewManagedMaterial(tt.input)
			_, err := m.Files()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestFilesIncludesTLSAndOmitsSystemd(t *testing.T) {
	t.Parallel()

	etcDir := t.TempDir()
	varDir := t.TempDir()

	m := NewManagedMaterial(Input{
		Paths: Paths{
			EtcDir: etcDir,
			VarDir: varDir,
			// SystemdDir deliberately empty to cover the skip branch.
		},
		PanelAuthToken:    "token",
		PanelListen:       "127.0.0.1:2096",
		PanelTLSEnabled:   true,
		PanelTLSCertPEM:   "cert-content",
		PanelTLSKeyPEM:    "key-content",
		InstallPanelCaddy: false,
	})

	files, err := m.Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}

	wantPaths := []string{
		filepath.Join(etcDir, "panel", "tls.crt"),
		filepath.Join(etcDir, "panel", "tls.key"),
		filepath.Join(etcDir, "veil.env"),
	}

	for _, want := range wantPaths {
		if !hasFile(files, want) {
			t.Fatalf("files missing %q:\n%+v", want, files)
		}
	}

	for _, f := range files {
		switch f.Path {
		case filepath.Join(etcDir, "panel", "tls.crt"):
			if f.Content != "cert-content" || f.Mode != 0o644 {
				t.Fatalf("unexpected cert file: %+v", f)
			}
		case filepath.Join(etcDir, "panel", "tls.key"):
			if f.Content != "key-content" || f.Mode != 0o600 {
				t.Fatalf("unexpected key file: %+v", f)
			}
		}
	}

	notWant := filepath.Join(etcDir, "generated", "caddy", "config.json")
	if hasFile(files, notWant) {
		t.Fatalf("did not expect Caddy JSON when InstallPanelCaddy is false, found %q", notWant)
	}
}

func TestFallbackIndexHTMLDefaultDomain(t *testing.T) {
	t.Parallel()

	got := fallbackIndexHTML("")
	if !strings.Contains(got, "<title>Veil</title>") {
		t.Fatalf("expected default title Veil, got:\n%s", got)
	}
}
