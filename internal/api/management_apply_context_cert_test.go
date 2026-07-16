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
	applyRoot := t.TempDir()
	statePath := filepath.Join(applyRoot, "state.json")
	if err := os.WriteFile(statePath, []byte(`{"inbounds":[],"warp":{}}`), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	state := newManagementState(ServerInfo{Mode: "dev", Privileged: client, StatePath: statePath, ApplyRoot: applyRoot})
	state.settings.PanelAccess = "caddy"
	state.settings.PanelDomain = "vpn.example.com"
	state.settings.PanelEmail = "admin@example.com"
	state.inbounds = []Inbound{{
		Name:           "hy2",
		Protocol:       "hysteria2",
		Transport:      "udp",
		Port:           443,
		Enabled:        true,
		ProtocolFields: map[string]any{"domain": "hy2.example.com"},
	}}

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

	ctx := NewManagementApplyContext(state)
	results := ctx.reloadPromotedServicesLocked([]string{hyPath, caddyPath})

	if len(client.syncCaddyCertRequests) != 1 {
		t.Fatalf("expected 1 sync request, got %+v", client.syncCaddyCertRequests)
	}
	want := privileged.SyncCaddyCertRequest{Domain: "hy2.example.com", OutDir: "/etc/veil/certs"}
	if !reflect.DeepEqual(client.syncCaddyCertRequests[0], want) {
		t.Fatalf("sync request = %+v, want %+v", client.syncCaddyCertRequests[0], want)
	}
	// The cert sync result should come after the Caddy admin/reload result.
	if len(results) < 2 || !results[0].Success {
		t.Fatalf("expected successful Caddy action first, got %+v", results)
	}
	caddyFound := false
	for _, r := range results {
		if r.Name == unitCaddy {
			caddyFound = true
			break
		}
	}
	if !caddyFound {
		t.Fatalf("expected Caddy service action in results, got %+v", results)
	}
}

func TestReloadPromotedServicesSkipsCertSyncWithoutHysteria2Domain(t *testing.T) {
	client := &recordingPrivilegedClient{}
	applyRoot := t.TempDir()
	statePath := filepath.Join(applyRoot, "state.json")
	if err := os.WriteFile(statePath, []byte(`{"inbounds":[],"warp":{}}`), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	state := newManagementState(ServerInfo{Mode: "dev", Privileged: client, StatePath: statePath, ApplyRoot: applyRoot})
	state.settings.PanelAccess = "local"
	state.inbounds = []Inbound{{
		Name:      "hy2",
		Protocol:  "hysteria2",
		Transport: "udp",
		Port:      443,
		Enabled:   true,
	}}

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

	if len(client.syncCaddyCertRequests) != 0 {
		t.Fatalf("expected no sync requests without hysteria2 domain, got %+v", client.syncCaddyCertRequests)
	}
}
