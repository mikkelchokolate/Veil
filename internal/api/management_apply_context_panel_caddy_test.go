package api

import (
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/privileged"
)

func TestReloadPromotedServicesReloadsPanelCaddyWithoutNaiveInbound(t *testing.T) {
	state := newManagementState(ServerInfo{Version: "test", Mode: "dev"})
	state.applyRoot = t.TempDir()
	state.liveRoot = filepath.Join(state.applyRoot, "live")
	state.settings.PanelAccess = "caddy"
	state.settings.PanelListen = "127.0.0.1:2096"
	state.settings.WebBasePath = "/panel/"
	state.settings.Domain = "panel.example.com"
	state.settings.Email = "admin@example.com"
	state.inbounds = []Inbound{{Name: "mieru-tcp", Protocol: "mieru", Transport: "tcp", Port: 8443, Enabled: true}}
	client := &recordingPrivilegedClient{}
	state.privileged = client
	state.privilegedLocal = false

	ctx := NewManagementApplyContext(state)
	results := ctx.reloadPromotedServicesLocked([]string{filepath.Join(state.liveRoot, "caddy", "panel.Caddyfile")})
	found := false
	for _, r := range results {
		if r.Name == "veil-caddy@panel.service" {
			found = true
			if !r.Success {
				t.Fatalf("expected success for panel caddy reload, got %+v", r)
			}
			if len(r.Command) < 2 || r.Command[1] != string(privileged.ServiceActionReload) {
				t.Fatalf("expected reload action, got %+v", r)
			}
		}
	}
	if !found {
		t.Fatalf("expected panel caddy reload action in results, got %+v", results)
	}
}
