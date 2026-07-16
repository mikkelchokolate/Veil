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
	results := ctx.reloadPromotedServicesLocked([]string{caddyPath})
	found := false
	for _, r := range results {
		if r.Name == unitCaddy {
			found = true
			if !r.Success {
				t.Fatalf("expected success for panel caddy reload, got %+v", r)
			}
			if len(r.Command) < 2 || (r.Command[1] != string(privileged.ServiceActionReload) && r.Command[1] != "admin") {
				t.Fatalf("expected reload or admin load action, got %+v", r)
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

	results := NewManagementApplyContext(state).reloadPromotedServicesLocked([]string{caddyPath})
	caddyResults := 0
	for _, result := range results {
		if !result.Success {
			t.Fatalf("successful systemd fallback must supersede Admin API miss: %+v", results)
		}
		if result.Name == unitCaddy {
			caddyResults++
		}
	}
	if caddyResults != 1 {
		t.Fatalf("expected one successful Caddy result, got %+v", results)
	}
	if len(client.serviceActions) != 1 || client.serviceActions[0].Unit != unitCaddy || client.serviceActions[0].Action != privileged.ServiceActionReload {
		t.Fatalf("expected one Caddy reload fallback, got %+v", client.serviceActions)
	}
}
