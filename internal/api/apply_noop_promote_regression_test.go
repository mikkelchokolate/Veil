package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/privileged"
)

// TestPromoteStagedConfigsSkipsByteIdenticalLiveArtifact locks the
// evidence-honesty half of #1134: a re-apply whose staged bytes are already
// live is a no-op. The artifact must not be re-promoted — no privileged
// promote call, no live files, no promotion records — so the apply does not
// falsify LiveApplied/ArtifactsChanged and does not restart units whose
// configuration did not change.
func TestPromoteStagedConfigsSkipsByteIdenticalLiveArtifact(t *testing.T) {
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
	// Byte-identical live artifact: the desired content is already there.
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

	liveFiles, backupFiles, records, err := ctx.promoteStagedConfigs([]string{staged})
	if err != nil {
		t.Fatalf("promote staged configs: %v", err)
	}
	if len(liveFiles) != 0 || len(records) != 0 || len(backupFiles) != 0 {
		t.Fatalf("byte-identical artifact must not promote: live=%v records=%v backups=%v", liveFiles, records, backupFiles)
	}
	if len(client.promotions) != 0 {
		t.Fatalf("byte-identical artifact reached the privileged promote path: %+v", client.promotions)
	}

	// A drifted artifact still promotes normally.
	if err := os.WriteFile(staged, []byte("config-v2"), 0o600); err != nil {
		t.Fatal(err)
	}
	liveFiles, _, records, err = ctx.promoteStagedConfigs([]string{staged})
	if err != nil {
		t.Fatalf("promote drifted config: %v", err)
	}
	if len(liveFiles) != 1 || len(records) != 1 {
		t.Fatalf("drifted artifact must promote: live=%v records=%v", liveFiles, records)
	}
	if len(client.promotions) != 1 {
		t.Fatalf("drifted artifact never reached promote: %+v", client.promotions)
	}
}

// TestFilterByteIdenticalArtifactsFailTowardChanged locks the conservative
// side of #1134: anything the panel cannot prove identical stays in the
// promoted set — unreadable live files (root-owned trees), missing
// destinations, and non-regular files are all treated as changed.
func TestFilterByteIdenticalArtifactsFailTowardChanged(t *testing.T) {
	root := t.TempDir()
	generated := filepath.Join(root, "generated")
	liveRoot := filepath.Join(root, "live")
	for _, dir := range []string{
		filepath.Join(generated, "a"), filepath.Join(liveRoot, "a"),
		filepath.Join(generated, "b"), filepath.Join(liveRoot, "b"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// a/identical.yaml: same bytes staged and live — filtered out.
	write(filepath.Join(generated, "a", "identical.yaml"), "same")
	write(filepath.Join(liveRoot, "a", "identical.yaml"), "same")
	// a/drifted.yaml: different bytes — kept.
	write(filepath.Join(generated, "a", "drifted.yaml"), "new")
	write(filepath.Join(liveRoot, "a", "drifted.yaml"), "old")
	// b/missing.yaml: no live counterpart — kept.
	write(filepath.Join(generated, "b", "missing.yaml"), "new")

	got := filterByteIdenticalArtifacts(generated, liveRoot, []string{
		"a/identical.yaml", "a/drifted.yaml", "b/missing.yaml", "b/not-staged.yaml",
	})
	want := []string{"a/drifted.yaml", "b/missing.yaml", "b/not-staged.yaml"}
	if len(got) != len(want) {
		t.Fatalf("filterByteIdenticalArtifacts = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("filterByteIdenticalArtifacts = %v, want %v", got, want)
		}
	}
}
