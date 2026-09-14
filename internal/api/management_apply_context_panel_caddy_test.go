package api

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/privileged"
)

func TestReloadPromotedServicesReloadsPanelCaddyWithoutNaiveInbound(t *testing.T) {
	oldCaddyLoader := caddyAdminLoader
	defer func() { caddyAdminLoader = oldCaddyLoader }()
	caddyAdminLoader = func(_ []byte) error { return nil }

	state := newManagementState(ServerInfo{Version: "test", Mode: "dev"})
	state.applyRoot = t.TempDir()
	state.liveRoot = filepath.Join(state.applyRoot, "live")
	state.settings.PanelAccess = "caddy"
	state.settings.PanelListen = "127.0.0.1:2096"
	state.settings.PanelDomain = "panel.example.com"
	state.settings.PanelEmail = "admin@example.com"
	state.settings.WebBasePath = "/panel/"
	state.inbounds = []Inbound{{Name: "mieru-tcp", Protocol: "mieru", Transport: "tcp", Port: 8443, Enabled: true}}
	client := &recordingPrivilegedClient{}
	state.privileged = client
	state.privilegedLocal = false

	caddyPath := filepath.Join(state.liveRoot, "caddy", "config.json")
	if err := os.MkdirAll(filepath.Dir(caddyPath), 0o755); err != nil {
		t.Fatalf("mkdir caddy: %v", err)
	}
	if err := os.WriteFile(caddyPath, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write caddy config: %v", err)
	}

	ctx := NewManagementApplyContext(state)
	results := ctx.reloadPromotedServices([]string{caddyPath})
	found := false
	for _, r := range results {
		if r.Name == unitCaddy {
			found = true
			if !r.Success {
				t.Fatalf("expected success for panel caddy reload, got %+v", r)
			}
			if len(r.Command) < 2 {
				t.Fatalf("expected a Caddy command, got %+v", r)
			}
			switch r.Command[1] {
			case string(privileged.ServiceActionReload), string(privileged.ServiceActionEnable), "admin":
			default:
				t.Fatalf("unexpected Caddy action, got %+v", r)
			}
		}
	}
	if !found {
		t.Fatalf("expected panel caddy reload action in results, got %+v", results)
	}
}

func TestReloadPromotedServicesTreatsSuccessfulCaddyFallbackAsSuccess(t *testing.T) {
	oldCaddyLoader := caddyAdminLoader
	defer func() { caddyAdminLoader = oldCaddyLoader }()
	caddyAdminLoader = func(_ []byte) error { return errors.New("admin unavailable") }

	state := newManagementState(ServerInfo{Version: "test", Mode: "dev"})
	state.applyRoot = t.TempDir()
	state.liveRoot = filepath.Join(state.applyRoot, "live")
	state.settings.PanelAccess = "caddy"
	state.settings.PanelListen = "127.0.0.1:2096"
	state.settings.PanelDomain = "panel.example.com"
	state.settings.PanelEmail = "admin@example.com"
	client := &recordingPrivilegedClient{}
	state.privileged = client
	state.privilegedLocal = false

	caddyPath := filepath.Join(state.liveRoot, "caddy", "config.json")
	if err := os.MkdirAll(filepath.Dir(caddyPath), 0o755); err != nil {
		t.Fatalf("mkdir caddy: %v", err)
	}
	if err := os.WriteFile(caddyPath, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write caddy config: %v", err)
	}

	results := NewManagementApplyContext(state).reloadPromotedServices([]string{caddyPath})
	caddyResults := 0
	for _, result := range results {
		if !result.Success {
			t.Fatalf("successful systemd fallback must supersede Admin API miss: %+v", results)
		}
		if result.Name == unitCaddy {
			caddyResults++
		}
	}
	if caddyResults < 1 {
		t.Fatalf("expected a successful Caddy result, got %+v", results)
	}
	if len(client.serviceActions) < 1 || client.serviceActions[0].Unit != unitCaddy || client.serviceActions[0].Action != privileged.ServiceActionReload {
		t.Fatalf("expected Caddy reload fallback first, got %+v", client.serviceActions)
	}
	foundEnable := false
	for _, action := range client.serviceActions {
		if action.Unit == unitCaddy && action.Action == privileged.ServiceActionEnable {
			foundEnable = true
		}
	}
	if !foundEnable {
		t.Fatalf("expected Caddy enable after reload, got %+v", client.serviceActions)
	}
}
