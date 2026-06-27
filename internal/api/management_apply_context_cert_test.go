package api

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/privileged"
)

func TestReloadPromotedServicesSyncsCaddyCertBeforeHysteria2(t *testing.T) {
	client := &recordingPrivilegedClient{}
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(statePath, []byte(`{"inbounds":[],"warp":{}}`), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	state := newManagementState(ServerInfo{Mode: "dev", Privileged: client, StatePath: statePath})
	state.settings.PanelAccess = "caddy"
	state.settings.Domain = "vpn.example.com"
	state.settings.Email = "admin@example.com"
	state.liveRoot = filepath.Join(state.applyRoot, "live")

	liveRoot := state.liveRoot
	hyPath := filepath.Join(liveRoot, "hysteria2", "server.yaml")
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
	want := privileged.SyncCaddyCertRequest{Domain: "vpn.example.com", OutDir: "/etc/veil/certs"}
	if !reflect.DeepEqual(client.syncCaddyCertRequests[0], want) {
		t.Fatalf("sync request = %+v, want %+v", client.syncCaddyCertRequests[0], want)
	}
	if len(results) == 0 || !results[0].Success {
		t.Fatalf("expected successful cert sync result, got %+v", results)
	}
}

func TestReloadPromotedServicesSkipsCertSyncWhenPanelAccessIsLocal(t *testing.T) {
	client := &recordingPrivilegedClient{}
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(statePath, []byte(`{"inbounds":[],"warp":{}}`), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	state := newManagementState(ServerInfo{Mode: "dev", Privileged: client, StatePath: statePath})
	state.settings.PanelAccess = "local"
	state.settings.Domain = "vpn.example.com"
	state.liveRoot = filepath.Join(state.applyRoot, "live")

	liveRoot := state.liveRoot
	hyPath := filepath.Join(liveRoot, "hysteria2", "server.yaml")
	if err := os.MkdirAll(filepath.Dir(hyPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(hyPath, []byte("config"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	ctx := NewManagementApplyContext(state)
	_ = ctx.reloadPromotedServicesLocked([]string{hyPath})

	if len(client.syncCaddyCertRequests) != 0 {
		t.Fatalf("expected no sync requests for local access, got %+v", client.syncCaddyCertRequests)
	}
}
