package api

import (
	"os"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/testguard"
	"github.com/mikkelchokolate/Veil/internal/testutil/fakecaddy"
)

// TestMain makes the systemd status reader hermetic by default: tests must not
// depend on whatever units happen to run on the host executing them. Tests that
// need specific statuses override serviceStatusReader and restore it.
// It also arms the production-path guard: any test that falls back to a
// production default path (/etc/veil, /var/lib/veil, ...) fails immediately
// instead of touching the live system when run as root.
func TestMain(m *testing.M) {
	provisionApplyRuntimes = func(ManagementApplyContext) error { return nil }
	serviceStatusReader = func(unit string) ServiceRuntimeStatus {
		return ServiceRuntimeStatus{Unit: unit, LoadState: "not-found", ActiveState: "inactive", SubState: "dead"}
	}
	// Caddy's Admin API is a process-external mutation boundary just like
	// systemd and the privileged helper. CI can share the host's Caddy, so the
	// production default (the /var/lib/caddy/admin.sock Unix socket) must never
	// be reachable from a unit test that happens to exercise apply. Tests for
	// Admin API behavior explicitly override this seam and restore it to this
	// hermetic baseline.
	caddyAdminLoader = func([]byte) error { return nil }

	// The Caddy capability probe resolves `caddy` through the trusted system
	// directories since #1458, so a unit test must not depend on the host (or
	// CI's PATH) carrying a real binary. Register a hermetic stub ahead of the
	// system dirs; tests that need a missing or specific probe override the
	// probeCaddyCapabilities seam themselves.
	caddyCleanup := fakecaddy.MustInstall()

	// Establish a package-wide filesystem boundary before any test runs. Tests
	// may override individual variables with t.Setenv, which restores them to
	// these isolated baselines instead of to the host's production defaults.
	testRoot, err := os.MkdirTemp("", "veil-api-test-")
	if err != nil {
		panic("create API test root: " + err.Error())
	}
	// Subprocess helpers (VEIL_IDEMPOTENCY_PROCESS) must inherit the parent's
	// VEIL_* roots verbatim: they open the same state/database files, and
	// hostenv-derived bases (e.g. the fallbackRoot allowlist under <etc>/www)
	// must resolve identically in both processes.
	isolatedEnv := map[string]string{
		"VEIL_STATE_PATH":       testRoot + "/state.json",
		"VEIL_KEY_PATH":         testRoot + "/state.key",
		"VEIL_APPLY_ROOT":       testRoot + "/staging",
		"VEIL_LIVE_ROOT":        testRoot + "/live",
		"VEIL_HELPER_SOCKET":    testRoot + "/helper.sock",
		"VEIL_AUTO_TLS_DIR":     testRoot + "/autocert",
		"VEIL_BACKUP_MAX_BYTES": "8388608",
	}
	if os.Getenv("VEIL_IDEMPOTENCY_PROCESS") == "" {
		for name, value := range isolatedEnv {
			if err := os.Setenv(name, value); err != nil {
				panic("set API test environment " + name + ": " + err.Error())
			}
		}
	}

	testguard.SetHookForTests(func(path string) {
		panic("unit test attempted to use production path: " + path)
	})
	code := m.Run()
	caddyCleanup()
	testguard.SetHookForTests(nil)
	if err := os.RemoveAll(testRoot); err != nil && code == 0 {
		panic("remove API test root: " + err.Error())
	}
	os.Exit(code)
}

// isolateCatalogEnv points the env-fallback runtime catalog at an isolated
// directory so tests calling NewVisibleManagedRuntimeCatalog(ForState(nil))
// never touch production defaults (/var/lib/veil/state.json,
// /etc/veil/state.key).
func isolateCatalogEnv(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("VEIL_STATE_PATH", dir+"/state.json")
	t.Setenv("VEIL_KEY_PATH", dir+"/state.key")
}
