package api

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/privileged"
	"github.com/mikkelchokolate/Veil/internal/renderer"
)

func TestReloadPromotedServicesLoadsCaddyBeforeHysteria2CertSync(t *testing.T) {
	client := &recordingPrivilegedClient{}
	applyRoot := t.TempDir()
	statePath := filepath.Join(applyRoot, "state.json")
	if err := os.WriteFile(statePath, []byte(`{"inbounds":[],"warp":{}}`), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	state := newManagementState(ServerInfo{Mode: "dev", Privileged: client, StatePath: statePath, ApplyRoot: applyRoot})
	state.settings.PanelAccess = "local"
	state.inbounds = []Inbound{{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true, ProtocolFields: map[string]any{"domain": "hy2.example.com"}}}

	liveRoot := state.liveRoot
	hyPath := filepath.Join(liveRoot, "hysteria2", "hy2.yaml")
	caddyPath := filepath.Join(liveRoot, "caddy", "config.json")
	if err := os.MkdirAll(filepath.Dir(hyPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(hyPath, []byte("config"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(caddyPath), 0o755); err != nil {
		t.Fatalf("mkdir caddy: %v", err)
	}
	if err := os.WriteFile(caddyPath, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write caddy config: %v", err)
	}

	oldLoader := caddyAdminLoader
	caddyAdminLoader = func(_ []byte) error { return nil }
	t.Cleanup(func() { caddyAdminLoader = oldLoader })

	ctx := NewManagementApplyContext(state)
	results := ctx.reloadPromotedServicesLocked([]string{hyPath, caddyPath})

	caddyIndex, syncIndex := -1, -1
	for i, r := range results {
		if r.Name == renderer.UnitCaddy && len(r.Command) >= 3 && r.Command[0] == "caddy" && r.Command[1] == "admin" && r.Command[2] == "load" {
			caddyIndex = i
		}
		if r.Name == "sync-caddy-cert" {
			syncIndex = i
		}
	}
	if caddyIndex == -1 {
		t.Fatalf("expected Caddy admin load result, got %+v", results)
	}
	if syncIndex == -1 {
		t.Fatalf("expected hysteria2 cert sync result, got %+v", results)
	}
	if syncIndex <= caddyIndex {
		t.Fatalf("expected hysteria2 cert sync (index %d) after Caddy admin load (index %d); results=%+v", syncIndex, caddyIndex, results)
	}
}

func TestReloadPromotedServicesSyncsCaddyCertBeforeHysteria2(t *testing.T) {
	client := &recordingPrivilegedClient{}
	applyRoot := t.TempDir()
	statePath := filepath.Join(applyRoot, "state.json")
	if err := os.WriteFile(statePath, []byte(`{"inbounds":[],"warp":{}}`), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	state := newManagementState(ServerInfo{Mode: "dev", Privileged: client, StatePath: statePath, ApplyRoot: applyRoot})
	state.settings.PanelAccess = "caddy"
	state.settings.Domain = "vpn.example.com"
	state.settings.Email = "admin@example.com"
	state.inbounds = []Inbound{{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true, ProtocolFields: map[string]any{"domain": "hy2.example.com"}}}

	liveRoot := state.liveRoot
	hyPath := filepath.Join(liveRoot, "hysteria2", "hy2.yaml")
	if err := os.MkdirAll(filepath.Dir(hyPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(hyPath, []byte("config"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	ctx := NewManagementApplyContext(state)
	results := ctx.reloadPromotedServicesLocked([]string{hyPath})

	if len(client.syncCaddyCertRequests) != 1 {
		t.Fatalf("expected 1 sync request, got %+v", client.syncCaddyCertRequests)
	}
	want := privileged.SyncCaddyCertRequest{Domain: "hy2.example.com", OutDir: "/etc/veil/certs"}
	if !reflect.DeepEqual(client.syncCaddyCertRequests[0], want) {
		t.Fatalf("sync request = %+v, want %+v", client.syncCaddyCertRequests[0], want)
	}
	if len(results) == 0 || !results[0].Success {
		t.Fatalf("expected successful cert sync result, got %+v", results)
	}
}

func TestReloadPromotedServicesSyncsCaddyCertRegardlessOfPanelAccess(t *testing.T) {
	client := &recordingPrivilegedClient{}
	applyRoot := t.TempDir()
	statePath := filepath.Join(applyRoot, "state.json")
	if err := os.WriteFile(statePath, []byte(`{"inbounds":[],"warp":{}}`), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	state := newManagementState(ServerInfo{Mode: "dev", Privileged: client, StatePath: statePath, ApplyRoot: applyRoot})
	state.settings.PanelAccess = "local"
	state.settings.Domain = "vpn.example.com"
	state.inbounds = []Inbound{{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true, ProtocolFields: map[string]any{"domain": "hy2.example.com"}}}

	liveRoot := state.liveRoot
	hyPath := filepath.Join(liveRoot, "hysteria2", "hy2.yaml")
	if err := os.MkdirAll(filepath.Dir(hyPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(hyPath, []byte("config"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	ctx := NewManagementApplyContext(state)
	_ = ctx.reloadPromotedServicesLocked([]string{hyPath})

	if len(client.syncCaddyCertRequests) != 1 {
		t.Fatalf("expected 1 sync request regardless of panel access, got %+v", client.syncCaddyCertRequests)
	}
}
