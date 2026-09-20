package privileged

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Regression for #307: a restore mixes two record kinds — pre-existing
// configs are written back, while configs newly added by the failed apply are
// deleted. The result must report them separately (WrittenArtifacts vs
// RemovedArtifacts); merging them makes the API reload a service against a
// config that no longer exists and skip stopping the newly added unit.
func TestRestorePromotedArtifactsSeparatesRestoredAndRemoved(t *testing.T) {
	stubRuntimeArtifactOwnership(t)

	root := t.TempDir()
	backupRoot := filepath.Join(root, "backups")
	backupID := "20260716T120000.000000000Z"
	backupDir := filepath.Join(backupRoot, backupID)

	caddyDst := filepath.Join(root, "generated", "caddy", "config.json")
	hy2Dst := filepath.Join(root, "generated", "hysteria2", "edge.yaml")
	oldCaddy := []byte(`{"apps":{"old":true}}`)
	writeFileWithParents(t, caddyDst, []byte(`{"apps":{"new":true}}`))
	writeFileWithParents(t, hy2Dst, []byte("new-edge"))
	safety := filepath.Join(backupDir, "files", "caddy-config.json")
	writeFileWithParents(t, safety, oldCaddy)

	manifest := promotionManifest{
		BackupID: backupID,
		Records: []promotionManifestRecord{
			{
				ArtifactID: "caddy/config.json", Destination: caddyDst,
				HadPrevious: true, SafetyPath: safety, BackupPath: safety,
				OldDigest: promotionDigest(oldCaddy),
			},
			{
				ArtifactID: "hysteria2/edge.yaml", Destination: hy2Dst,
				HadPrevious: false,
			},
		},
	}
	body, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "manifest.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := restorePromotedArtifacts(backupRoot, backupID)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !reflect.DeepEqual(result.WrittenArtifacts, []string{"caddy/config.json"}) {
		t.Fatalf("restored artifacts = %v, want [caddy/config.json]", result.WrittenArtifacts)
	}
	if !reflect.DeepEqual(result.RemovedArtifacts, []string{"hysteria2/edge.yaml"}) {
		t.Fatalf("removed artifacts = %v, want [hysteria2/edge.yaml]", result.RemovedArtifacts)
	}
	assertFileContent(t, caddyDst, string(oldCaddy))
	if _, err := os.Stat(hy2Dst); !os.IsNotExist(err) {
		t.Fatalf("newly added config should be deleted by restore, stat err=%v", err)
	}
}
