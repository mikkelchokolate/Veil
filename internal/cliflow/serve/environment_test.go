package serve

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/privileged"
	"github.com/mikkelchokolate/Veil/internal/secrets"
)

func TestEnvironmentResolvesAuthPathsAndWebBasePath(t *testing.T) {
	env := NewEnvironment()
	token, tokenSource := env.AuthToken("secret")
	statePath, stateSource := env.StatePath("/state.json")
	webBasePath, webBasePathSource := env.WebBasePath("secret")
	if token != "secret" || tokenSource != "--auth-token" {
		t.Fatalf("auth = %q %q", token, tokenSource)
	}
	if statePath != "/state.json" || stateSource != "--state" {
		t.Fatalf("state = %q %q", statePath, stateSource)
	}
	if webBasePath != "/secret/" || webBasePathSource != "--web-base-path" {
		t.Fatalf("web base path = %q %q", webBasePath, webBasePathSource)
	}
}

func TestEnvironmentResolvesMetricsAccessPolicy(t *testing.T) {
	env := NewEnvironment()

	access, source := env.MetricsAccess("authenticated")
	if access != "authenticated" || source != "--metrics-access" {
		t.Fatalf("metrics access = %q %q", access, source)
	}

	required, err := env.MetricsAuthRequired("auto", true, "VEIL_API_TOKEN", true)
	if err != nil {
		t.Fatalf("auto metrics policy: %v", err)
	}
	if !required {
		t.Fatalf("auto metrics policy should require auth on public listen")
	}

	required, err = env.MetricsAuthRequired("public", false, "disabled", false)
	if err != nil {
		t.Fatalf("local public metrics policy: %v", err)
	}
	if required {
		t.Fatalf("public metrics policy should not require auth on local listen")
	}

	if _, err := env.MetricsAuthRequired("public", true, "VEIL_API_TOKEN", true); err == nil {
		t.Fatalf("expected public metrics policy to be rejected on public listen")
	}
}

func TestEnvironmentListenResolvesSources(t *testing.T) {
	env := NewEnvironment()

	listen, source := env.Listen("")
	if listen != "127.0.0.1:2096" || source != "default" {
		t.Fatalf("default listen = %q %q", listen, source)
	}

	t.Setenv("VEIL_LISTEN", "0.0.0.0:8080")
	listen, source = env.Listen("")
	if listen != "0.0.0.0:8080" || source != "VEIL_LISTEN" {
		t.Fatalf("env listen = %q %q", listen, source)
	}

	listen, source = env.Listen("  192.168.1.1:9090  ")
	if listen != "192.168.1.1:9090" || source != "--listen" {
		t.Fatalf("flag listen = %q %q", listen, source)
	}
}

func TestEnvironmentAuthTokenResolvesSources(t *testing.T) {
	env := NewEnvironment()

	token, source := env.AuthToken("")
	if token != "" || source != "disabled" {
		t.Fatalf("default auth = %q %q", token, source)
	}

	t.Setenv("VEIL_API_TOKEN", "env-token")
	token, source = env.AuthToken("")
	if token != "env-token" || source != "VEIL_API_TOKEN" {
		t.Fatalf("env auth = %q %q", token, source)
	}

	token, source = env.AuthToken("  flag-token  ")
	if token != "flag-token" || source != "--auth-token" {
		t.Fatalf("flag auth = %q %q", token, source)
	}
}

func TestEnvironmentValidateListen(t *testing.T) {
	env := NewEnvironment()

	cases := []struct {
		name    string
		listen  string
		wantErr string
	}{
		{"valid", "127.0.0.1:2096", ""},
		{"missing host", ":2096", "listen address must include a host"},
		{"invalid port", "127.0.0.1:abc", "invalid port"},
		{"port out of range", "127.0.0.1:99999", "must be 1-65535"},
		{"no port", "127.0.0.1", "host:port"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := env.ValidateListen(tc.listen)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestEnvironmentValidateAuthBinding(t *testing.T) {
	env := NewEnvironment()

	if err := env.ValidateAuthBinding("127.0.0.1:2096", "disabled"); err != nil {
		t.Fatalf("loopback without token should be allowed: %v", err)
	}
	if err := env.ValidateAuthBinding("0.0.0.0:2096", "disabled"); err == nil {
		t.Fatalf("expected public listen without token to be rejected")
	}
	if err := env.ValidateAuthBinding("0.0.0.0:2096", "VEIL_API_TOKEN"); err != nil {
		t.Fatalf("public listen with token should be allowed: %v", err)
	}
	if err := env.ValidateAuthBinding("bad-address", "disabled"); err == nil {
		t.Fatalf("expected invalid listen to be rejected")
	}
}

func TestEnvironmentValidatePublicExposure(t *testing.T) {
	env := NewEnvironment()

	cases := []struct {
		name                  string
		listen                string
		tokenSource           string
		sessionAuthConfigured bool
		wantErr               string
	}{
		{"loopback", "127.0.0.1:2096", "disabled", false, ""},
		{"public fully configured", "0.0.0.0:2096", "VEIL_API_TOKEN", true, ""},
		{"public missing token", "0.0.0.0:2096", "disabled", true, "API token auth"},
		{"public missing session", "0.0.0.0:2096", "VEIL_API_TOKEN", false, "user/session auth"},
		{"public missing both", "0.0.0.0:2096", "disabled", false, "API token auth"},
		{"invalid listen", "bad-address", "disabled", false, "host:port"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := env.ValidatePublicExposure(tc.listen, tc.tokenSource, tc.sessionAuthConfigured)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestEnvironmentIsPublicListen(t *testing.T) {
	env := NewEnvironment()

	cases := []struct {
		name       string
		listen     string
		wantPublic bool
		wantErr    bool
	}{
		{"127.0.0.1", "127.0.0.1:2096", false, false},
		{"localhost", "localhost:2096", false, false},
		{"0.0.0.0", "0.0.0.0:2096", true, false},
		{"public IP", "203.0.113.1:2096", true, false},
		{"invalid", "bad-address", false, true},
		{"bad port", "127.0.0.1:99999", false, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			public, err := env.IsPublicListen(tc.listen)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if public != tc.wantPublic {
				t.Fatalf("public = %v, want %v", public, tc.wantPublic)
			}
		})
	}
}

func TestEnvironmentIsLoopbackHost(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"127.0.0.1", true},
		{"localhost", true},
		{"LOCALHOST", true},
		{"::1", true},
		{"0.0.0.0", false},
		{"203.0.113.1", false},
		{"not-an-ip", false},
	}

	for _, tc := range cases {
		t.Run(tc.host, func(t *testing.T) {
			if got := isLoopbackHost(tc.host); got != tc.want {
				t.Fatalf("isLoopbackHost(%q) = %v, want %v", tc.host, got, tc.want)
			}
		})
	}
}

func TestEnvironmentSessionAuthConfigured(t *testing.T) {
	env := NewEnvironment()

	t.Run("missing file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing.json")
		configured, err := env.SessionAuthConfigured(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if configured {
			t.Fatalf("expected no session auth for missing file")
		}
	})

	t.Run("empty users", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.json")
		store := managementstate.NewStore(path, nil)
		if err := store.Save(model.ManagementSnapshot{}); err != nil {
			t.Fatalf("save: %v", err)
		}
		configured, err := env.SessionAuthConfigured(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if configured {
			t.Fatalf("expected no session auth for empty users")
		}
	})

	t.Run("with users", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.json")
		store := managementstate.NewStore(path, nil)
		if err := store.Save(model.ManagementSnapshot{Users: []model.User{{Username: "admin"}}}); err != nil {
			t.Fatalf("save: %v", err)
		}
		configured, err := env.SessionAuthConfigured(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !configured {
			t.Fatalf("expected session auth for users")
		}
	})

	t.Run("corrupted state", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.json")
		if err := os.WriteFile(path, []byte("not valid json"), 0600); err != nil {
			t.Fatalf("write: %v", err)
		}
		_, err := env.SessionAuthConfigured(path)
		if err == nil {
			t.Fatalf("expected error for corrupted state")
		}
	})
}

func TestEnvironmentMetricsAccessResolvesSources(t *testing.T) {
	env := NewEnvironment()

	access, source := env.MetricsAccess("")
	if access != "auto" || source != "default" {
		t.Fatalf("default metrics = %q %q", access, source)
	}

	t.Setenv("VEIL_METRICS_ACCESS", "PUBLIC")
	access, source = env.MetricsAccess("")
	if access != "public" || source != "VEIL_METRICS_ACCESS" {
		t.Fatalf("env metrics = %q %q", access, source)
	}

	access, source = env.MetricsAccess(" Authenticated ")
	if access != "authenticated" || source != "--metrics-access" {
		t.Fatalf("flag metrics = %q %q", access, source)
	}
}

func TestEnvironmentMetricsAuthRequired(t *testing.T) {
	env := NewEnvironment()

	cases := []struct {
		name                  string
		access                string
		publicListen          bool
		tokenSource           string
		sessionAuthConfigured bool
		wantRequired          bool
		wantErr               string
	}{
		{"auto local no auth", "auto", false, "disabled", false, false, ""},
		{"auto public", "auto", true, "disabled", false, true, ""},
		{"auto token", "auto", false, "VEIL_API_TOKEN", false, true, ""},
		{"auto session", "auto", false, "disabled", true, true, ""},
		{"authenticated", "authenticated", false, "disabled", false, true, ""},
		{"public local", "public", false, "disabled", false, false, ""},
		{"public public", "public", true, "disabled", false, false, "/metrics cannot be public"},
		{"invalid", "invalid", false, "disabled", false, false, "must be one of"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			required, err := env.MetricsAuthRequired(tc.access, tc.publicListen, tc.tokenSource, tc.sessionAuthConfigured)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if required != tc.wantRequired {
				t.Fatalf("required = %v, want %v", required, tc.wantRequired)
			}
		})
	}
}

func TestEnvironmentPathResolvers(t *testing.T) {
	env := NewEnvironment()
	root := t.TempDir()

	// Other tests that build an HTTP server mutate the process environment
	// through api.newManagementState. Ensure this test sees clean defaults.
	t.Setenv("VEIL_STATE_PATH", "")
	t.Setenv("VEIL_APPLY_ROOT", "")
	t.Setenv("VEIL_LIVE_ROOT", "")
	t.Setenv("VEIL_KEY_PATH", "")
	t.Setenv("VEIL_HELPER_SOCKET", "")

	assertFlag := func(name string, flagValue, envName, got, source, want, wantSource string) {
		t.Helper()
		if got != want || source != wantSource {
			t.Fatalf("%s(flag=%q) = %q %q, want %q %q", name, flagValue, got, source, want, wantSource)
		}
	}

	assertDefault := func(name, got, source, want, wantSource string) {
		t.Helper()
		if got != want || source != wantSource {
			t.Fatalf("%s(default) = %q %q, want %q %q", name, got, source, want, wantSource)
		}
	}

	t.Run("StatePath", func(t *testing.T) {
		path, source := env.StatePath(filepath.Join(root, "flag.json"))
		assertFlag("StatePath", "flag", "VEIL_STATE_PATH", path, source, filepath.Join(root, "flag.json"), "--state")

		t.Setenv("VEIL_STATE_PATH", filepath.Join(root, "env.json"))
		path, source = env.StatePath("")
		assertFlag("StatePath", "", "VEIL_STATE_PATH", path, source, filepath.Join(root, "env.json"), "VEIL_STATE_PATH")
		t.Setenv("VEIL_STATE_PATH", "")

		path, source = env.StatePath("")
		if source != "default" {
			t.Fatalf("expected default source, got %q", source)
		}
		if runtime.GOOS == "windows" {
			pd := os.Getenv("ProgramData")
			if pd == "" {
				pd = `C:\ProgramData`
			}
			assertDefault("StatePath", path, source, filepath.Join(pd, "Veil", "state.json"), "default")
		} else {
			assertDefault("StatePath", path, source, "/var/lib/veil/state.json", "default")
		}
	})

	t.Run("ApplyRoot", func(t *testing.T) {
		path, source := env.ApplyRoot(filepath.Join(root, "flag"))
		assertFlag("ApplyRoot", "flag", "VEIL_APPLY_ROOT", path, source, filepath.Join(root, "flag"), "--apply-root")

		t.Setenv("VEIL_APPLY_ROOT", filepath.Join(root, "env"))
		path, source = env.ApplyRoot("")
		assertFlag("ApplyRoot", "", "VEIL_APPLY_ROOT", path, source, filepath.Join(root, "env"), "VEIL_APPLY_ROOT")
		t.Setenv("VEIL_APPLY_ROOT", "")

		path, source = env.ApplyRoot("")
		if runtime.GOOS == "windows" {
			pd := os.Getenv("ProgramData")
			if pd == "" {
				pd = `C:\ProgramData`
			}
			assertDefault("ApplyRoot", path, source, filepath.Join(pd, "Veil"), "default")
		} else {
			assertDefault("ApplyRoot", path, source, "/var/lib/veil/staging", "default")
		}
	})

	t.Run("LiveRoot", func(t *testing.T) {
		path, source := env.LiveRoot(filepath.Join(root, "flag"))
		assertFlag("LiveRoot", "flag", "VEIL_LIVE_ROOT", path, source, filepath.Join(root, "flag"), "--live-root")

		t.Setenv("VEIL_LIVE_ROOT", filepath.Join(root, "env"))
		path, source = env.LiveRoot("")
		assertFlag("LiveRoot", "", "VEIL_LIVE_ROOT", path, source, filepath.Join(root, "env"), "VEIL_LIVE_ROOT")
		t.Setenv("VEIL_LIVE_ROOT", "")

		path, source = env.LiveRoot("")
		if runtime.GOOS == "windows" {
			pd := os.Getenv("ProgramData")
			if pd == "" {
				pd = `C:\ProgramData`
			}
			assertDefault("LiveRoot", path, source, filepath.Join(pd, "Veil", "live"), "default")
		} else {
			assertDefault("LiveRoot", path, source, "/etc/veil/generated", "default")
		}
	})

	t.Run("KeyPath", func(t *testing.T) {
		path, source := env.KeyPath(filepath.Join(root, "flag.key"))
		assertFlag("KeyPath", "flag", "VEIL_KEY_PATH", path, source, filepath.Join(root, "flag.key"), "--key-path")

		t.Setenv("VEIL_KEY_PATH", filepath.Join(root, "env.key"))
		path, source = env.KeyPath("")
		assertFlag("KeyPath", "", "VEIL_KEY_PATH", path, source, filepath.Join(root, "env.key"), "VEIL_KEY_PATH")
		t.Setenv("VEIL_KEY_PATH", "")

		path, source = env.KeyPath("")
		if runtime.GOOS == "windows" {
			pd := os.Getenv("ProgramData")
			if pd == "" {
				pd = `C:\ProgramData`
			}
			assertDefault("KeyPath", path, source, filepath.Join(pd, "Veil", "state.key"), "default")
		} else {
			assertDefault("KeyPath", path, source, "/etc/veil/state.key", "default")
		}
	})

	t.Run("windows defaults", func(t *testing.T) {
		oldGoos := goos
		goos = "windows"
		defer func() { goos = oldGoos }()

		pd := filepath.Join(root, "ProgramData")
		t.Setenv("ProgramData", pd)

		path, _ := env.StatePath("")
		if path != filepath.Join(pd, "Veil", "state.json") {
			t.Fatalf("windows state path = %q", path)
		}
		path, _ = env.ApplyRoot("")
		if path != filepath.Join(pd, "Veil") {
			t.Fatalf("windows apply root = %q", path)
		}
		path, _ = env.LiveRoot("")
		if path != filepath.Join(pd, "Veil", "live") {
			t.Fatalf("windows live root = %q", path)
		}
		path, _ = env.KeyPath("")
		if path != filepath.Join(pd, "Veil", "state.key") {
			t.Fatalf("windows key path = %q", path)
		}
	})

	t.Run("windows defaults without ProgramData", func(t *testing.T) {
		oldGoos := goos
		goos = "windows"
		defer func() { goos = oldGoos }()

		t.Setenv("ProgramData", "")
		path, _ := env.StatePath("")
		want := filepath.Join(`C:\ProgramData`, "Veil", "state.json")
		if path != want {
			t.Fatalf("windows state path without ProgramData = %q, want %q", path, want)
		}
		path, _ = env.ApplyRoot("")
		want = filepath.Join(`C:\ProgramData`, "Veil")
		if path != want {
			t.Fatalf("windows apply root without ProgramData = %q, want %q", path, want)
		}
		path, _ = env.LiveRoot("")
		want = filepath.Join(`C:\ProgramData`, "Veil", "live")
		if path != want {
			t.Fatalf("windows live root without ProgramData = %q, want %q", path, want)
		}
		path, _ = env.KeyPath("")
		want = filepath.Join(`C:\ProgramData`, "Veil", "state.key")
		if path != want {
			t.Fatalf("windows key path without ProgramData = %q, want %q", path, want)
		}
	})
}

func TestEnvironmentHelperSocket(t *testing.T) {
	env := NewEnvironment()

	if path, source := env.HelperSocket("/custom/helper.sock"); path != "/custom/helper.sock" || source != "--helper-socket" {
		t.Fatalf("flag path=%q source=%q", path, source)
	}
	t.Setenv("VEIL_HELPER_SOCKET", "/env/helper.sock")
	if path, source := env.HelperSocket(""); path != "/env/helper.sock" || source != "VEIL_HELPER_SOCKET" {
		t.Fatalf("env path=%q source=%q", path, source)
	}
	t.Setenv("VEIL_HELPER_SOCKET", "")
	if path, source := env.HelperSocket(""); path != privileged.DefaultSocketPath || source != "default" {
		t.Fatalf("default path=%q source=%q", path, source)
	}
}

func TestEnvironmentPanelAccessDomainEmail(t *testing.T) {
	env := NewEnvironment()

	t.Setenv("VEIL_PANEL_ACCESS", "caddy")
	t.Setenv("VEIL_DOMAIN", "example.com")
	t.Setenv("VEIL_EMAIL", "admin@example.com")

	if got := env.PanelAccess(); got != "caddy" {
		t.Fatalf("PanelAccess = %q", got)
	}
	if got := env.Domain(); got != "example.com" {
		t.Fatalf("Domain = %q", got)
	}
	if got := env.Email(); got != "admin@example.com" {
		t.Fatalf("Email = %q", got)
	}
}

func TestEnvironmentAllowUnsafePublicHTTP(t *testing.T) {
	env := NewEnvironment()

	allowed, err := env.AllowUnsafePublicHTTP(true)
	if err != nil || !allowed {
		t.Fatalf("flag true: allowed=%v err=%v", allowed, err)
	}

	allowed, err = env.AllowUnsafePublicHTTP(false)
	if err != nil || allowed {
		t.Fatalf("default false: allowed=%v err=%v", allowed, err)
	}

	t.Setenv("VEIL_UNSAFE_ALLOW_PUBLIC_HTTP", "true")
	allowed, err = env.AllowUnsafePublicHTTP(false)
	if err != nil || !allowed {
		t.Fatalf("env true: allowed=%v err=%v", allowed, err)
	}

	t.Setenv("VEIL_UNSAFE_ALLOW_PUBLIC_HTTP", "not-a-bool")
	_, err = env.AllowUnsafePublicHTTP(false)
	if err == nil || !strings.Contains(err.Error(), "boolean") {
		t.Fatalf("expected boolean parse error, got %v", err)
	}
}

func TestEnvironmentWebBasePath(t *testing.T) {
	env := NewEnvironment()

	cases := []struct {
		flag   string
		want   string
		source string
	}{
		{"", "/", "default"},
		{"admin", "/admin/", "--web-base-path"},
		{"/admin/", "/admin/", "--web-base-path"},
		{"admin/", "/admin/", "--web-base-path"},
		{"/admin", "/admin/", "--web-base-path"},
	}

	for _, tc := range cases {
		t.Run(tc.flag, func(t *testing.T) {
			got, source := env.WebBasePath(tc.flag)
			if got != tc.want || source != tc.source {
				t.Fatalf("WebBasePath(%q) = %q %q, want %q %q", tc.flag, got, source, tc.want, tc.source)
			}
		})
	}

	t.Setenv("VEIL_WEB_BASE_PATH", "panel")
	got, source := env.WebBasePath("")
	if got != "/panel/" || source != "VEIL_WEB_BASE_PATH" {
		t.Fatalf("env web base path = %q %q", got, source)
	}
}

func TestEnvironmentTLS(t *testing.T) {
	env := NewEnvironment()

	enabled, source := env.TLS("/flag.crt", "/flag.key")
	if !enabled || source != "--tls-cert / --tls-key" {
		t.Fatalf("flag TLS = %v %q", enabled, source)
	}

	enabled, _ = env.TLS("/flag.crt", "")
	if enabled {
		t.Fatalf("expected TLS to be disabled with missing key")
	}

	t.Setenv("VEIL_TLS_CERT", "/env.crt")
	t.Setenv("VEIL_TLS_KEY", "/env.key")
	enabled, source = env.TLS("", "")
	if !enabled || source != "VEIL_TLS_CERT / VEIL_TLS_KEY" {
		t.Fatalf("env TLS = %v %q", enabled, source)
	}
	t.Setenv("VEIL_TLS_KEY", "")
	enabled, _ = env.TLS("", "")
	if enabled {
		t.Fatalf("expected TLS to be disabled with missing env key")
	}

	t.Setenv("VEIL_TLS_CERT", "")
	enabled, source, certPath, keyPath := env.TLSFiles("", "")
	if enabled || source != "" || certPath != "" || keyPath != "" {
		t.Fatalf("default TLS = %v %q %q %q", enabled, source, certPath, keyPath)
	}
}

func TestEnvironmentAutoTLS(t *testing.T) {
	env := NewEnvironment()
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	keyPath := filepath.Join(root, "state.key")

	t.Run("disabled", func(t *testing.T) {
		cfg, err := env.AutoTLS(false, "", statePath, keyPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Enabled {
			t.Fatalf("expected auto-tls to be disabled")
		}
	})

	t.Run("enabled via env", func(t *testing.T) {
		store := managementstate.NewStore(statePath, nil)
		if err := store.Save(model.ManagementSnapshot{Settings: model.Settings{Domain: "example.com", Email: "admin@example.com"}}); err != nil {
			t.Fatalf("save: %v", err)
		}
		t.Setenv("VEIL_AUTO_TLS", "1")
		cfg, err := env.AutoTLS(false, "", statePath, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !cfg.Enabled || cfg.Domain != "example.com" || cfg.Email != "admin@example.com" {
			t.Fatalf("unexpected cfg: %+v", cfg)
		}
		t.Setenv("VEIL_AUTO_TLS", "")
	})

	t.Run("missing domain", func(t *testing.T) {
		store := managementstate.NewStore(statePath, nil)
		if err := store.Save(model.ManagementSnapshot{Settings: model.Settings{Email: "admin@example.com"}}); err != nil {
			t.Fatalf("save: %v", err)
		}
		_, err := env.AutoTLS(true, "", statePath, "")
		if err == nil || !strings.Contains(err.Error(), "domain") {
			t.Fatalf("expected domain error, got %v", err)
		}
	})

	t.Run("missing email", func(t *testing.T) {
		store := managementstate.NewStore(statePath, nil)
		if err := store.Save(model.ManagementSnapshot{Settings: model.Settings{Domain: "example.com"}}); err != nil {
			t.Fatalf("save: %v", err)
		}
		_, err := env.AutoTLS(true, "", statePath, "")
		if err == nil || !strings.Contains(err.Error(), "email") {
			t.Fatalf("expected email error, got %v", err)
		}
	})

	t.Run("custom cache dir", func(t *testing.T) {
		store := managementstate.NewStore(statePath, nil)
		if err := store.Save(model.ManagementSnapshot{Settings: model.Settings{Domain: "example.com", Email: "admin@example.com"}}); err != nil {
			t.Fatalf("save: %v", err)
		}
		cacheDir := filepath.Join(root, "autocert")
		cfg, err := env.AutoTLS(true, cacheDir, statePath, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.CacheDir != cacheDir {
			t.Fatalf("cache dir = %q, want %q", cfg.CacheDir, cacheDir)
		}
	})

	t.Run("env cache dir", func(t *testing.T) {
		store := managementstate.NewStore(statePath, nil)
		if err := store.Save(model.ManagementSnapshot{Settings: model.Settings{Domain: "example.com", Email: "admin@example.com"}}); err != nil {
			t.Fatalf("save: %v", err)
		}
		envDir := filepath.Join(root, "env-autocert")
		t.Setenv("VEIL_AUTO_TLS_DIR", envDir)
		cfg, err := env.AutoTLS(true, "", statePath, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.CacheDir != envDir {
			t.Fatalf("cache dir = %q, want %q", cfg.CacheDir, envDir)
		}
		t.Setenv("VEIL_AUTO_TLS_DIR", "")
	})

	t.Run("default cache dir", func(t *testing.T) {
		store := managementstate.NewStore(statePath, nil)
		if err := store.Save(model.ManagementSnapshot{Settings: model.Settings{Domain: "example.com", Email: "admin@example.com"}}); err != nil {
			t.Fatalf("save: %v", err)
		}
		cfg, err := env.AutoTLS(true, "", statePath, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := "/var/lib/veil/autocert"
		if runtime.GOOS == "windows" {
			pd := os.Getenv("ProgramData")
			if pd == "" {
				pd = `C:\ProgramData`
			}
			want = filepath.Join(pd, "Veil", "autocert")
		}
		if cfg.CacheDir != want {
			t.Fatalf("cache dir = %q, want %q", cfg.CacheDir, want)
		}
	})

	t.Run("settings error", func(t *testing.T) {
		_, err := env.AutoTLS(true, "", filepath.Join(root, "missing.json"), "")
		if err == nil || !strings.Contains(err.Error(), "failed to read state") {
			t.Fatalf("expected settings error, got %v", err)
		}
	})

	t.Run("windows default cache dir", func(t *testing.T) {
		oldGoos := goos
		goos = "windows"
		defer func() { goos = oldGoos }()

		pd := filepath.Join(root, "ProgramData")
		t.Setenv("ProgramData", pd)

		store := managementstate.NewStore(statePath, nil)
		if err := store.Save(model.ManagementSnapshot{Settings: model.Settings{Domain: "example.com", Email: "admin@example.com"}}); err != nil {
			t.Fatalf("save: %v", err)
		}
		cfg, err := env.AutoTLS(true, "", statePath, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := filepath.Join(pd, "Veil", "autocert")
		if cfg.CacheDir != want {
			t.Fatalf("cache dir = %q, want %q", cfg.CacheDir, want)
		}
	})

	t.Run("windows default cache dir without ProgramData", func(t *testing.T) {
		oldGoos := goos
		goos = "windows"
		defer func() { goos = oldGoos }()

		t.Setenv("ProgramData", "")
		store := managementstate.NewStore(statePath, nil)
		if err := store.Save(model.ManagementSnapshot{Settings: model.Settings{Domain: "example.com", Email: "admin@example.com"}}); err != nil {
			t.Fatalf("save: %v", err)
		}
		cfg, err := env.AutoTLS(true, "", statePath, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := filepath.Join(`C:\ProgramData`, "Veil", "autocert")
		if cfg.CacheDir != want {
			t.Fatalf("cache dir = %q, want %q", cfg.CacheDir, want)
		}
	})
}

func TestEnvironmentSettingsFromState(t *testing.T) {
	env := NewEnvironment()
	root := t.TempDir()

	t.Run("plain JSON", func(t *testing.T) {
		statePath := filepath.Join(root, "plain.json")
		data, _ := json.Marshal(model.ManagementSnapshot{Settings: model.Settings{Domain: "example.com", Email: "admin@example.com"}})
		if err := os.WriteFile(statePath, data, 0600); err != nil {
			t.Fatalf("write: %v", err)
		}
		domain, email, err := env.SettingsFromState(statePath, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if domain != "example.com" || email != "admin@example.com" {
			t.Fatalf("domain=%q email=%q", domain, email)
		}
	})

	t.Run("encrypted", func(t *testing.T) {
		statePath := filepath.Join(root, "encrypted.json")
		keyPath := filepath.Join(root, "encrypted.key")
		key, err := secrets.LoadOrCreateKey(keyPath)
		if err != nil {
			t.Fatalf("load key: %v", err)
		}
		cipher, err := secrets.NewCipher(*key)
		if err != nil {
			t.Fatalf("new cipher: %v", err)
		}
		store := managementstate.NewStore(statePath, cipher)
		if err := store.Save(model.ManagementSnapshot{Settings: model.Settings{Domain: "encrypted.example.com", Email: "enc@example.com"}}); err != nil {
			t.Fatalf("save: %v", err)
		}
		domain, email, err := env.SettingsFromState(statePath, keyPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if domain != "encrypted.example.com" || email != "enc@example.com" {
			t.Fatalf("domain=%q email=%q", domain, email)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		_, _, err := env.SettingsFromState(filepath.Join(root, "missing.json"), "")
		if err == nil {
			t.Fatalf("expected error for missing file")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		statePath := filepath.Join(root, "invalid.json")
		if err := os.WriteFile(statePath, []byte("not json"), 0600); err != nil {
			t.Fatalf("write: %v", err)
		}
		_, _, err := env.SettingsFromState(statePath, "")
		if err == nil || !strings.Contains(err.Error(), "parse state JSON") {
			t.Fatalf("expected JSON parse error, got %v", err)
		}
	})
}

func TestEnvironmentValidatePort(t *testing.T) {
	cases := []struct {
		port    string
		wantErr bool
	}{
		{"1", false},
		{"65535", false},
		{"0", true},
		{"65536", true},
		{"abc", true},
	}

	for _, tc := range cases {
		t.Run(tc.port, func(t *testing.T) {
			err := validatePort(tc.port)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for port %q", tc.port)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for port %q: %v", tc.port, err)
			}
		})
	}
}

func TestCleanWebBasePath(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		// Note: CleanWebBasePath appends a trailing slash unconditionally,
		// so the root case produces "//". This matches the current behavior.
		{"", "//"},
		{"/", "//"},
		{"admin", "/admin/"},
		{"/admin", "/admin/"},
		{"admin/", "/admin/"},
		{"/admin/", "/admin/"},
		{"//admin//", "/admin/"},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			if got := CleanWebBasePath(tc.input); got != tc.want {
				t.Fatalf("CleanWebBasePath(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
