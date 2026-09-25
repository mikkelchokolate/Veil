package privileged

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Regression for #1005: the restore backup ID reaches the helper through
// persisted promotion records, so it must be a canonical single path
// segment. The opaque pattern alone admits ".", "..", and names containing
// "..", which resolve the manifest outside the backup root — a manifest
// planted there is trusted for root-level write/delete/symlink operations.
func TestPolicyRejectsNonCanonicalPromotionRestoreID(t *testing.T) {
	policy := testPolicy(t)
	for _, id := range []string{"..", ".", "a..b", "...", "..hidden"} {
		if _, err := policy.ResolvePromotion(PromoteRequest{RestoreBackupID: id}); err == nil {
			t.Fatalf("restore backup id %q unexpectedly resolved", id)
		} else {
			assertOperationErrorCode(t, err, ErrorInvalidRequest)
		}
	}
	resolved, err := policy.ResolvePromotion(PromoteRequest{RestoreBackupID: "20260605T120000.000000000Z"})
	if err != nil || resolved.RestoreBackupID != "20260605T120000.000000000Z" {
		t.Fatalf("canonical restore id rejected: %v %+v", err, resolved)
	}
	resolved, err = policy.ResolvePromotion(PromoteRequest{RestoreBackupID: "rollback-20260605T120000.000000000Z-0f47ac10-5c1d-4c2b-9a3b-7b7f8f4f9a01"})
	if err != nil || resolved.RestoreBackupID == "" {
		t.Fatalf("rollback-style restore id rejected: %v", err)
	}
}

// Regression for #1005: with a ".." backup id the manifest path resolves to
// the backup root's parent, and a manifest planted there drives privileged
// deletes/writes. The executor must reject the id before reading anything.
func TestRestorePromotedArtifactsRejectsTraversalBackupID(t *testing.T) {
	stubRuntimeArtifactOwnership(t)
	root := t.TempDir()
	backupRoot := filepath.Join(root, "promotion-backups")
	if err := os.MkdirAll(backupRoot, 0o700); err != nil {
		t.Fatal(err)
	}

	// A record whose destination is deleted by the restore — planting this
	// manifest one level up would previously let ".." delete it as root.
	victim := filepath.Join(root, "generated", "victim.conf")
	writeFileWithParents(t, victim, []byte("keep"))
	manifest := promotionManifest{
		Version: 1, TransactionID: "planted", BackupID: "..",
		Records: []promotionManifestRecord{{
			ArtifactID: "victim.conf", Destination: victim, HadPrevious: false,
		}},
	}
	body, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"..", "."} {
		if _, err := restorePromotedArtifacts(backupRoot, id); err == nil {
			t.Fatalf("restore with traversal backup id %q succeeded", id)
		}
	}
	assertFileContent(t, victim, "keep")
}
