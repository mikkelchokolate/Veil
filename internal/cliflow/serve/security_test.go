package serve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestServeSecurityResolvesSafeConfig(t *testing.T) {
	security := NewSecurity(SecurityOptions{
		Listen:      "127.0.0.1:2096",
		AuthToken:   "secret",
		StatePath:   "/state.json",
		ApplyRoot:   "/apply",
		KeyPath:     "/state.key",
		WebBasePath: "secret",
	})
	cfg, err := security.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cfg.Token != "secret" || cfg.TokenSource != "--auth-token" || cfg.WebBasePath != "/secret/" {
		t.Fatalf("config = %+v", cfg)
	}
	if !cfg.SetupAllowed {
		t.Fatalf("local loopback config should allow first-run setup: %+v", cfg)
	}
}

func TestServeSecurityRejectsPublicListenWithoutSessionUsersEvenWithToken(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	security := NewSecurity(SecurityOptions{
		Listen:    "0.0.0.0:2096",
		AuthToken: "secret",
		StatePath: statePath,
	})

	_, err := security.Resolve()
	if err == nil {
		t.Fatalf("expected public listen without session users to be rejected")
	}
	if !strings.Contains(err.Error(), "veil admin reset") || !strings.Contains(err.Error(), "session") {
		t.Fatalf("expected first-run session setup guidance, got %v", err)
	}
}

func TestServeSecurityAllowsPublicListenWithTokenAndSessionUser(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	store := managementstate.NewStore(statePath, nil)
	if err := store.Save(model.ManagementSnapshot{
		Settings: model.Settings{PanelListen: "0.0.0.0:2096", Mode: "server"},
		Users: []model.User{
			{Username: "admin", PasswordHash: "hash", Role: "admin"},
		},
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	security := NewSecurity(SecurityOptions{
		Listen:    "0.0.0.0:2096",
		AuthToken: "secret",
		StatePath: statePath,
		TLSCert:   "/tmp/panel.crt",
		TLSKey:    "/tmp/panel.key",
	})

	cfg, err := security.Resolve()
	if err != nil {
		t.Fatalf("expected public listen with token and session user to be allowed: %v", err)
	}
	if !cfg.PublicListen || !cfg.SessionAuthConfigured || !cfg.MetricsAuthRequired {
		t.Fatalf("expected public/session/metrics auth flags, got %+v", cfg)
	}
}

func TestServeSecurityRejectsPublicListenWithoutTLS(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := managementstate.NewStore(statePath, nil).Save(model.ManagementSnapshot{
		Users: []model.User{{Username: "admin", PasswordHash: "hash", Role: "admin"}},
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	_, err := NewSecurity(SecurityOptions{
		Listen:    "0.0.0.0:2096",
		AuthToken: "secret",
		StatePath: statePath,
	}).Resolve()
	if err == nil || !strings.Contains(err.Error(), "TLS") {
		t.Fatalf("expected public HTTP to be rejected, got %v", err)
	}
}

func TestServeSecurityTreatsCaddyAsPublicExposure(t *testing.T) {
	t.Setenv("VEIL_PANEL_ACCESS", "caddy")
	statePath := filepath.Join(t.TempDir(), "state.json")

	_, err := NewSecurity(SecurityOptions{StatePath: statePath}).Resolve()
	if err == nil || !strings.Contains(err.Error(), "user/session") {
		t.Fatalf("expected Caddy without users to be rejected, got %v", err)
	}

	if err := managementstate.NewStore(statePath, nil).Save(model.ManagementSnapshot{
		Users: []model.User{{Username: "admin", PasswordHash: "hash", Role: "admin"}},
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}
	cfg, err := NewSecurity(SecurityOptions{StatePath: statePath}).Resolve()
	if err != nil {
		t.Fatalf("expected configured Caddy exposure: %v", err)
	}
	if !cfg.MetricsAuthRequired || !cfg.SessionAuthConfigured {
		t.Fatalf("expected authenticated Caddy exposure, got %+v", cfg)
	}
	if cfg.SetupAllowed {
		t.Fatalf("Caddy exposure must not allow first-run setup: %+v", cfg)
	}
}

func TestServeSecurityRejectsInvalidListen(t *testing.T) {
	_, err := NewSecurity(SecurityOptions{Listen: "bad-address"}).Resolve()
	if err == nil || !strings.Contains(err.Error(), "host:port") {
		t.Fatalf("expected invalid listen error, got %v", err)
	}
}

func TestServeSecurityRejectsInvalidMetricsAccess(t *testing.T) {
	_, err := NewSecurity(SecurityOptions{
		Listen:        "127.0.0.1:2096",
		MetricsAccess: "invalid",
	}).Resolve()
	if err == nil || !strings.Contains(err.Error(), "metrics-access") {
		t.Fatalf("expected invalid metrics access error, got %v", err)
	}
}

func TestServeSecurityAutoTLS(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := managementstate.NewStore(statePath, nil).Save(model.ManagementSnapshot{
		Settings: model.Settings{Domain: "example.com", Email: "admin@example.com"},
		Users:    []model.User{{Username: "admin", PasswordHash: "hash", Role: "admin"}},
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	cfg, err := NewSecurity(SecurityOptions{
		Listen:    "0.0.0.0:2096",
		AuthToken: "secret",
		StatePath: statePath,
		AutoTLS:   true,
	}).Resolve()
	if err != nil {
		t.Fatalf("expected auto-tls config: %v", err)
	}
	if !cfg.TLSEnabled || cfg.TLSSource != "auto-tls (Let's Encrypt)" {
		t.Fatalf("expected auto-tls enabled, got %+v", cfg)
	}
	if cfg.AutoTLSDomain != "example.com" || cfg.AutoTLSEmail != "admin@example.com" {
		t.Fatalf("unexpected auto-tls config: %+v", cfg)
	}
}

func TestServeSecurityAutoTLSError(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := managementstate.NewStore(statePath, nil).Save(model.ManagementSnapshot{
		Users: []model.User{{Username: "admin", PasswordHash: "hash", Role: "admin"}},
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	_, err := NewSecurity(SecurityOptions{
		Listen:    "0.0.0.0:2096",
		AuthToken: "secret",
		StatePath: statePath,
		AutoTLS:   true,
	}).Resolve()
	if err == nil || !strings.Contains(err.Error(), "auto-tls") {
		t.Fatalf("expected auto-tls error, got %v", err)
	}
}

func TestServeSecurityAllowsUnsafePublicHTTP(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := managementstate.NewStore(statePath, nil).Save(model.ManagementSnapshot{
		Users: []model.User{{Username: "admin", PasswordHash: "hash", Role: "admin"}},
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	cfg, err := NewSecurity(SecurityOptions{
		Listen:                "0.0.0.0:2096",
		AuthToken:             "secret",
		StatePath:             statePath,
		AllowUnsafePublicHTTP: true,
	}).Resolve()
	if err != nil {
		t.Fatalf("expected unsafe public HTTP to be allowed: %v", err)
	}
	if !cfg.AllowUnsafePublicHTTP {
		t.Fatalf("expected AllowUnsafePublicHTTP to be true: %+v", cfg)
	}
}

func TestServeSecurityRejectsPublicMetrics(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := managementstate.NewStore(statePath, nil).Save(model.ManagementSnapshot{
		Users: []model.User{{Username: "admin", PasswordHash: "hash", Role: "admin"}},
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	_, err := NewSecurity(SecurityOptions{
		Listen:        "0.0.0.0:2096",
		AuthToken:     "secret",
		StatePath:     statePath,
		TLSCert:       "/tmp/panel.crt",
		TLSKey:        "/tmp/panel.key",
		MetricsAccess: "public",
	}).Resolve()
	if err == nil || !strings.Contains(err.Error(), "/metrics") {
		t.Fatalf("expected public metrics error, got %v", err)
	}
}

func TestServeSecurityHandlesSessionAuthCheckError(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(statePath, []byte("not valid json"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := NewSecurity(SecurityOptions{
		Listen:    "0.0.0.0:2096",
		AuthToken: "secret",
		StatePath: statePath,
	}).Resolve()
	if err == nil || !strings.Contains(err.Error(), "session auth check") {
		t.Fatalf("expected session auth check error, got %v", err)
	}
}

func TestServeSecurityRejectsInvalidUnsafePublicHTTPValue(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := managementstate.NewStore(statePath, nil).Save(model.ManagementSnapshot{
		Users: []model.User{{Username: "admin", PasswordHash: "hash", Role: "admin"}},
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	t.Setenv("VEIL_UNSAFE_ALLOW_PUBLIC_HTTP", "not-a-bool")
	_, err := NewSecurity(SecurityOptions{
		Listen:    "0.0.0.0:2096",
		AuthToken: "secret",
		StatePath: statePath,
	}).Resolve()
	if err == nil || !strings.Contains(err.Error(), "boolean") {
		t.Fatalf("expected boolean parse error, got %v", err)
	}
}

func TestServeSecurityEnvPathSources(t *testing.T) {
	root := t.TempDir()
	t.Setenv("VEIL_STATE_PATH", filepath.Join(root, "env-state.json"))
	t.Setenv("VEIL_APPLY_ROOT", filepath.Join(root, "env-apply"))
	t.Setenv("VEIL_LIVE_ROOT", filepath.Join(root, "env-live"))
	t.Setenv("VEIL_KEY_PATH", filepath.Join(root, "env.key"))
	t.Setenv("VEIL_HELPER_SOCKET", filepath.Join(root, "env.sock"))

	cfg, err := NewSecurity(SecurityOptions{
		Listen:       "127.0.0.1:2096",
		AuthToken:    "secret",
		StatePath:    filepath.Join(root, "flag-state.json"),
		ApplyRoot:    filepath.Join(root, "flag-apply"),
		LiveRoot:     filepath.Join(root, "flag-live"),
		KeyPath:      filepath.Join(root, "flag.key"),
		HelperSocket: filepath.Join(root, "flag.sock"),
		WebBasePath:  "/panel",
	}).Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if cfg.StatePath != filepath.Join(root, "flag-state.json") || cfg.StateSource != "--state" {
		t.Fatalf("state = %q %q", cfg.StatePath, cfg.StateSource)
	}
	if cfg.ApplyRoot != filepath.Join(root, "flag-apply") || cfg.ApplyRootSource != "--apply-root" {
		t.Fatalf("apply root = %q %q", cfg.ApplyRoot, cfg.ApplyRootSource)
	}
	if cfg.LiveRoot != filepath.Join(root, "flag-live") || cfg.LiveRootSource != "--live-root" {
		t.Fatalf("live root = %q %q", cfg.LiveRoot, cfg.LiveRootSource)
	}
	if cfg.KeyPath != filepath.Join(root, "flag.key") || cfg.KeySource != "--key-path" {
		t.Fatalf("key path = %q %q", cfg.KeyPath, cfg.KeySource)
	}
	if cfg.HelperSocket != filepath.Join(root, "flag.sock") || cfg.HelperSocketSource != "--helper-socket" {
		t.Fatalf("helper socket = %q %q", cfg.HelperSocket, cfg.HelperSocketSource)
	}
	if cfg.WebBasePath != "/panel/" {
		t.Fatalf("web base path = %q", cfg.WebBasePath)
	}
}
