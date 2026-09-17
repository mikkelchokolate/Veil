package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/privileged"
)

// Regression for #307: during rollback the hysteria2 certificate sync must
// use the domain of the restored config file, not the failed candidate
// inbounds. A candidate whose domain was changed in the failed apply would
// otherwise poll Caddy for a certificate the restored service never serves.
func TestRollbackSyncsRestoredHysteria2Domain(t *testing.T) {
	root := t.TempDir()
	liveRoot := filepath.Join(root, "live")
	livePath := filepath.Join(liveRoot, "hysteria2", "edge.yaml")
	certDir := filepath.Join(root, "certs")
	restored := "listen: ':443'\ntls:\n  cert: " + filepath.Join(certDir, "old.example.com.crt") +
		"\n  key: " + filepath.Join(certDir, "old.example.com.key") + "\nauth:\n  type: password\n  password: x\n"
	if err := os.MkdirAll(filepath.Dir(livePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(livePath, []byte(restored), 0o600); err != nil {
		t.Fatal(err)
	}

	client := &recordingPrivilegedClient{
		statusActiveState: "inactive",
		promoteResult: privileged.PromoteResult{
			BackupID:         "20260716T120000.000000000Z",
			WrittenArtifacts: []string{"hysteria2/edge.yaml"},
		},
	}
	state := newManagementState(ServerInfo{Mode: "dev", ApplyRoot: root, LiveRoot: liveRoot, Privileged: client})
	// The failed candidate changed the inbound's domain; the restored file
	// still serves the previous one.
	state.inbounds = []Inbound{{
		Name: "edge", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true,
		ProtocolFields: map[string]any{"domain": "new.example.com"},
	}}
	ctx := NewManagementApplyContext(state)

	records := []livePromotionRecord{{
		ArtifactID:  "hysteria2/edge.yaml",
		BackupID:    "20260716T120000.000000000Z",
		LivePath:    livePath,
		HadPrevious: true,
	}}
	_, actions := ctx.rollbackPromotedConfigs(records, []string{livePath})

	if len(client.syncCaddyCertRequests) != 1 {
		t.Fatalf("expected exactly one cert sync, got %+v (actions=%+v)", client.syncCaddyCertRequests, actions)
	}
	if got := client.syncCaddyCertRequests[0].Domain; got != "old.example.com" {
		t.Fatalf("cert sync domain = %q, want restored domain old.example.com", got)
	}
}

// A newly added inbound's config is deleted by the restore: no cert sync may
// run for its candidate-only domain, and its unit is stopped+disabled.
func TestRollbackSkipsCertSyncForDeletedNewConfig(t *testing.T) {
	root := t.TempDir()
	liveRoot := filepath.Join(root, "live")
	livePath := filepath.Join(liveRoot, "hysteria2", "edge.yaml")
	client := &recordingPrivilegedClient{
		statusActiveState: "inactive",
		promoteResult: privileged.PromoteResult{
			BackupID:         "20260716T120000.000000000Z",
			RemovedArtifacts: []string{"hysteria2/edge.yaml"},
		},
	}
	state := newManagementState(ServerInfo{Mode: "dev", ApplyRoot: root, LiveRoot: liveRoot, Privileged: client})
	state.inbounds = []Inbound{{
		Name: "edge", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true,
		ProtocolFields: map[string]any{"domain": "new.example.com"},
	}}
	ctx := NewManagementApplyContext(state)

	records := []livePromotionRecord{{
		ArtifactID: "hysteria2/edge.yaml",
		BackupID:   "20260716T120000.000000000Z",
		LivePath:   livePath,
	}}
	rollbackFiles, _ := ctx.rollbackPromotedConfigs(records, []string{livePath})

	if len(rollbackFiles) != 0 {
		t.Fatalf("deleted new config must not be reported as restored: %v", rollbackFiles)
	}
	if len(client.syncCaddyCertRequests) != 0 {
		t.Fatalf("cert sync must not run for a deleted new config: %+v", client.syncCaddyCertRequests)
	}
	stopped, disabled := false, false
	for _, a := range client.serviceActions {
		if a.Unit != "veil-hysteria2@edge.service" {
			continue
		}
		switch a.Action {
		case privileged.ServiceActionStop:
			stopped = true
		case privileged.ServiceActionDisable:
			disabled = true
		case privileged.ServiceActionStart, privileged.ServiceActionEnable:
			t.Fatalf("deleted new unit must not be started/enabled: %+v", client.serviceActions)
		}
	}
	if !stopped || !disabled {
		t.Fatalf("deleted new unit must be stopped+disabled: %+v", client.serviceActions)
	}
}
