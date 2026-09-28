package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/privileged"
)

// TestPromoteStagedConfigsPromotesByteIdenticalLiveArtifact locks the
// convergence contract for a re-apply whose staged bytes already match live:
// the artifact must still promote — liveFiles stays non-empty so service
// reload/enable runs, ServicesApplied can report true, and response.Applied
// stays true so the durable layer marks the revision live instead of parking
// desired>applied as "staged" forever. A byte-equality dedup here reads as a
// no-op apply but silently breaks applied-revision convergence
// (install-acceptance: applied=false on a converged apply).
func TestPromoteStagedConfigsPromotesByteIdenticalLiveArtifact(t *testing.T) {
	root := t.TempDir()
	staged := filepath.Join(root, "generated", "hysteria2", "edge.yaml")
	if err := os.MkdirAll(filepath.Dir(staged), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("config"), 0o600); err != nil {
		t.Fatal(err)
	}
	liveRoot := filepath.Join(root, "live")
	livePath := filepath.Join(liveRoot, "hysteria2", "edge.yaml")
	if err := os.MkdirAll(filepath.Dir(livePath), 0o755); err != nil {
		t.Fatal(err)
	}
	// Byte-identical live artifact — the desired content is already there.
	if err := os.WriteFile(livePath, []byte("config"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Report every probed unit as absent so the WARP/Caddy teardown gate does
	// not add removal legs to this artifact-only scenario.
	client := &recordingPrivilegedClient{
		statusActiveState:   "inactive",
		statusLoadState:     "not-found",
		statusUnitFileState: "disabled",
		promoteResult: privileged.PromoteResult{
			BackupID:         "20260608T120000.000000000Z",
			WrittenArtifacts: []string{"hysteria2/edge.yaml"},
		},
	}
	state := newManagementState(ServerInfo{Mode: "dev", ApplyRoot: root, LiveRoot: liveRoot, Privileged: client})
	state.inbounds = []Inbound{{Name: "edge", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true}}
	ctx := NewManagementApplyContext(state)

	liveFiles, _, records, err := ctx.promoteStagedConfigs([]string{staged})
	if err != nil {
		t.Fatalf("promote staged configs: %v", err)
	}
	if len(liveFiles) != 1 || liveFiles[0] != livePath {
		t.Fatalf("byte-identical artifact must still promote so services converge: live=%v", liveFiles)
	}
	if len(records) != 1 {
		t.Fatalf("expected one promotion record, got %+v", records)
	}
	if len(client.promotions) != 1 {
		t.Fatalf("byte-identical artifact never reached the privileged promote path: %+v", client.promotions)
	}
}
