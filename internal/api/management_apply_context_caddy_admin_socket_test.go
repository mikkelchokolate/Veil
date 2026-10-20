package api

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/privileged"
)

func caddyAdminSocketApplyState(t *testing.T, client *recordingPrivilegedClient) (*managementState, string) {
	t.Helper()
	state := newManagementState(ServerInfo{Version: "test", Mode: "dev"})
	state.applyRoot = t.TempDir()
	state.liveRoot = filepath.Join(state.applyRoot, "live")
	state.settings.PanelAccess = "caddy"
	state.settings.PanelListen = "127.0.0.1:2096"
	state.settings.PanelDomain = "panel.example.com"
	state.settings.PanelEmail = "admin@example.com"
	state.privileged = client
	state.privilegedLocal = false
	caddyPath := filepath.Join(state.liveRoot, "caddy", "config.json")
	if err := os.MkdirAll(filepath.Dir(caddyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(caddyPath, []byte(`{"admin":{"listen":"unix//var/lib/caddy/admin.sock|0200"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return state, caddyPath
}

func serviceActionTrace(client *recordingPrivilegedClient) []string {
	var got []string
	for _, action := range client.serviceActions {
		got = append(got, action.Unit+":"+string(action.Action))
	}
	return got
}

// #1276 migration: a Caddy started from a pre-#1276 config still serves its
// admin API on legacy loopback TCP, so the helper's Unix-socket load misses
// and `caddy reload` (ExecReload) dials the socket named by the NEW config
// and misses too. Without a restart the singleton could never pick up the
// socket-bound config; the apply must restart Caddy as the last resort and
// report that restart, not the failed reload, as the Caddy outcome.
func TestReloadPromotedServicesRestartsCaddyWhenAdminAndReloadMiss(t *testing.T) {
	oldCaddyLoader := caddyAdminLoader
	defer func() { caddyAdminLoader = oldCaddyLoader }()
	caddyAdminLoader = func([]byte) error {
		return errors.New("caddy admin POST failed: dial unix /var/lib/caddy/admin.sock: connect: no such file or directory")
	}
	client := &recordingPrivilegedClient{serviceActionErr: func(request privileged.ServiceActionRequest) error {
		if request.Unit == unitCaddy && request.Action == privileged.ServiceActionReload {
			return errors.New("caddy reload: dial unix /var/lib/caddy/admin.sock: connect: no such file or directory")
		}
		return nil
	}}
	state, caddyPath := caddyAdminSocketApplyState(t, client)

	results := NewManagementApplyContext(state).reloadPromotedServices([]string{caddyPath})

	want := []string{unitCaddy + ":reload", unitCaddy + ":restart", unitCaddy + ":enable"}
	if got := serviceActionTrace(client); !reflect.DeepEqual(got, want) {
		t.Fatalf("service actions = %v, want %v", got, want)
	}
	for _, result := range results {
		if !result.Success {
			t.Fatalf("a successful restart fallback must supersede the admin/reload misses: %+v", results)
		}
	}
	restarted := false
	for _, result := range results {
		if result.Name == unitCaddy && reflect.DeepEqual(result.Command, []string{"systemctl", "restart", unitCaddy}) {
			restarted = true
		}
	}
	if !restarted {
		t.Fatalf("expected the Caddy restart to be reported, got %+v", results)
	}
}

func TestReloadPromotedServicesReportsCaddyRestartFallbackFailure(t *testing.T) {
	oldCaddyLoader := caddyAdminLoader
	defer func() { caddyAdminLoader = oldCaddyLoader }()
	caddyAdminLoader = func([]byte) error { return errors.New("admin unavailable") }
	client := &recordingPrivilegedClient{serviceActionErr: func(request privileged.ServiceActionRequest) error {
		if request.Unit == unitCaddy {
			return errors.New(string(request.Action) + " failed")
		}
		return nil
	}}
	state, caddyPath := caddyAdminSocketApplyState(t, client)

	results := NewManagementApplyContext(state).reloadPromotedServices([]string{caddyPath})

	want := []string{unitCaddy + ":reload", unitCaddy + ":restart"}
	if got := serviceActionTrace(client); !reflect.DeepEqual(got, want) {
		t.Fatalf("service actions = %v, want %v (no enable after a failed activation)", got, want)
	}
	if len(results) == 0 {
		t.Fatal("expected a Caddy failure result")
	}
	last := results[len(results)-1]
	if last.Success || last.Name != unitCaddy {
		t.Fatalf("expected a failed Caddy result last, got %+v", results)
	}
	for _, part := range []string{"caddy admin load failed: ", "systemd fallback failed: reload failed", "restart fallback failed: restart failed"} {
		if !strings.Contains(last.Error, part) {
			t.Fatalf("failure must name %q, got %q", part, last.Error)
		}
	}
}

// A healthy reload after an admin miss keeps the no-restart path: the
// restart fallback must never drop Caddy's connections when reload works.
func TestReloadPromotedServicesDoesNotRestartCaddyWhenReloadSucceeds(t *testing.T) {
	oldCaddyLoader := caddyAdminLoader
	defer func() { caddyAdminLoader = oldCaddyLoader }()
	caddyAdminLoader = func([]byte) error { return errors.New("admin unavailable") }
	client := &recordingPrivilegedClient{}
	state, caddyPath := caddyAdminSocketApplyState(t, client)

	NewManagementApplyContext(state).reloadPromotedServices([]string{caddyPath})

	want := []string{unitCaddy + ":reload", unitCaddy + ":enable"}
	if got := serviceActionTrace(client); !reflect.DeepEqual(got, want) {
		t.Fatalf("service actions = %v, want %v", got, want)
	}
}
