package cli

import (
	"path/filepath"
	"testing"

	serveflow "github.com/mikkelchokolate/Veil/internal/cliflow/serve"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
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
	cfg, err := serveflow.NewSecurity(serveflow.SecurityOptions{}).Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !cfg.TLSEnabled || cfg.TLSCert != "/etc/veil/panel/tls.crt" || cfg.TLSKey != "/etc/veil/panel/tls.key" {
		t.Fatalf("cfg should carry TLS paths from env: %+v", cfg)
	}
}

func TestResolveServeConfigUsesResolvedPanelInstallEnvironment(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := managementstate.NewStore(statePath, nil).Save(model.ManagementSnapshot{
		Users: []model.User{{Username: "admin", PasswordHash: "hash", Role: "admin"}},
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}
	t.Setenv("VEIL_LISTEN", "127.0.0.1:34567")
	t.Setenv("VEIL_PANEL_ACCESS", "caddy")
	t.Setenv("VEIL_DOMAIN", "panel.example.com")
	t.Setenv("VEIL_EMAIL", "admin@example.com")
	t.Setenv("VEIL_STATE_PATH", statePath)
	cfg, err := serveflow.NewSecurity(serveflow.SecurityOptions{}).Resolve()
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
