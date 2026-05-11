package cli

import (
	"testing"

	serveflow "github.com/veil-panel/veil/internal/cliflow/serve"
)

func TestServeEnvironmentResolvesListenFromEnv(t *testing.T) {
	t.Setenv("VEIL_LISTEN", "127.0.0.1:34567")
	listen, source := serveflow.NewEnvironment().Listen("")
	if listen != "127.0.0.1:34567" || source != "VEIL_LISTEN" {
		t.Fatalf("listen = %q %q", listen, source)
	}
}

func TestResolveServeConfigCarriesTLSPathsFromEnv(t *testing.T) {
	t.Setenv("VEIL_TLS_CERT", "/etc/veil/panel/tls.crt")
	t.Setenv("VEIL_TLS_KEY", "/etc/veil/panel/tls.key")
	cfg, err := NewServeSecurity(serveWorkflowOptions{}).Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !cfg.TLSEnabled || cfg.TLSCert != "/etc/veil/panel/tls.crt" || cfg.TLSKey != "/etc/veil/panel/tls.key" {
		t.Fatalf("cfg should carry TLS paths from env: %+v", cfg)
	}
}

func TestResolveServeConfigUsesResolvedPanelInstallEnvironment(t *testing.T) {
	t.Setenv("VEIL_LISTEN", "127.0.0.1:34567")
	t.Setenv("VEIL_PANEL_ACCESS", "caddy")
	t.Setenv("VEIL_DOMAIN", "panel.example.com")
	t.Setenv("VEIL_EMAIL", "admin@example.com")
	cfg, err := NewServeSecurity(serveWorkflowOptions{}).Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cfg.Listen != "127.0.0.1:34567" || cfg.ListenSource != "VEIL_LISTEN" {
		t.Fatalf("cfg listen = %+v", cfg)
	}
	if cfg.PanelAccess != "caddy" || cfg.Domain != "panel.example.com" || cfg.Email != "admin@example.com" {
		t.Fatalf("cfg should carry Panel install environment: %+v", cfg)
	}
}
