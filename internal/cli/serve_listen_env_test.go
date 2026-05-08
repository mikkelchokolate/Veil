package cli

import "testing"

func TestServeEnvironmentResolvesListenFromEnv(t *testing.T) {
	t.Setenv("VEIL_LISTEN", "127.0.0.1:34567")
	listen, source := NewServeEnvironment().Listen("")
	if listen != "127.0.0.1:34567" || source != "VEIL_LISTEN" {
		t.Fatalf("listen = %q %q", listen, source)
	}
}

func TestResolveServeConfigUsesResolvedPanelInstallEnvironment(t *testing.T) {
	t.Setenv("VEIL_LISTEN", "127.0.0.1:34567")
	t.Setenv("VEIL_PANEL_ACCESS", "caddy")
	t.Setenv("VEIL_DOMAIN", "panel.example.com")
	t.Setenv("VEIL_EMAIL", "admin@example.com")
	cfg, err := resolveServeConfig(serveWorkflowOptions{})
	if err != nil {
		t.Fatalf("resolveServeConfig: %v", err)
	}
	if cfg.Listen != "127.0.0.1:34567" || cfg.ListenSource != "VEIL_LISTEN" {
		t.Fatalf("cfg listen = %+v", cfg)
	}
	if cfg.PanelAccess != "caddy" || cfg.Domain != "panel.example.com" || cfg.Email != "admin@example.com" {
		t.Fatalf("cfg should carry Panel install environment: %+v", cfg)
	}
}
