package privileged

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInterruptedLegacyCaddyfileRemovalDoesNotBlockLaterPromote(t *testing.T) {
	stubRuntimeArtifactOwnership(t)
	policy := testPolicy(t)
	backupRoot := filepath.Join(policy.StateRoot, "promotion-backups")
	legacyID := "caddy/legacy.Caddyfile"
	legacyDest := filepath.Join(policy.GeneratedRoot, filepath.FromSlash(legacyID))
	safetyPath := filepath.Join(backupRoot, "safety", filepath.FromSlash(legacyID))
	oldBody := []byte("legacy-caddyfile")
	if err := os.MkdirAll(filepath.Dir(legacyDest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(safetyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyDest, oldBody, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(safetyPath, oldBody, 0o600); err != nil {
		t.Fatal(err)
	}
	journal := promotionTransactionJournal{
		Version: 1, TransactionID: "legacy-tx", Kind: "promotion", Phase: "artifact-1-published",
		Manifest: promotionManifest{
			Version: 1, TransactionID: "legacy-tx", BackupID: "safety", Kind: "promotion", Phase: "prepared",
			Records: []promotionManifestRecord{{
				ArtifactID: legacyID, Destination: legacyDest, Operation: "remove",
				HadPrevious: true, SafetyPath: safetyPath, BackupPath: safetyPath,
				OldDigest: promotionDigest(oldBody), Phase: "published",
			}},
		},
	}
	if err := writePromotionJournal(backupRoot, journal); err != nil {
		t.Fatal(err)
	}

	source := filepath.Join(policy.StagingRoot, "caddy", "config.json")
	destination := filepath.Join(policy.GeneratedRoot, "caddy", "config.json")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte(`{"apps":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte(`{"apps":{"old":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	resolved, err := policy.ResolvePromotion(PromoteRequest{ArtifactIDs: []string{"caddy/config.json"}})
	if err != nil {
		t.Fatalf("resolve caddy/config.json: %v", err)
	}
	executor := NewProductionExecutor(ProductionConfig{PromotionBackupRoot: backupRoot})
	if _, err := executor.Promote(context.Background(), resolved); err != nil {
		t.Fatalf("later promote after leftover Caddyfile crash journal: %v", err)
	}
	body, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"apps":{}}` {
		t.Fatalf("promoted config.json=%s", body)
	}
	if _, err := os.Stat(filepath.Join(backupRoot, promotionTransactionJournalName)); !os.IsNotExist(err) {
		t.Fatalf("crash journal still present: %v", err)
	}
}

func TestInterruptedLegacyCaddyfileRestoreRecovers(t *testing.T) {
	stubRuntimeArtifactOwnership(t)
	policy := testPolicy(t)
	backupRoot := filepath.Join(policy.StateRoot, "promotion-backups")
	legacyID := "caddy/legacy.Caddyfile"
	legacyDest := filepath.Join(policy.GeneratedRoot, filepath.FromSlash(legacyID))
	safetyPath := filepath.Join(backupRoot, "safety", filepath.FromSlash(legacyID))
	oldBody := []byte("restored-caddyfile")
	if err := os.MkdirAll(filepath.Dir(legacyDest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(safetyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(safetyPath, oldBody, 0o600); err != nil {
		t.Fatal(err)
	}
	journal := promotionTransactionJournal{
		Version: 1, TransactionID: "restore-tx", Kind: "rollback", Phase: "artifact-1-published",
		Manifest: promotionManifest{
			Version: 1, TransactionID: "restore-tx", BackupID: "safety", Kind: "rollback", Phase: "prepared",
			Records: []promotionManifestRecord{{
				ArtifactID: legacyID, Destination: legacyDest, Operation: "write",
				HadPrevious: true, SafetyPath: safetyPath, BackupPath: safetyPath,
				OldDigest: promotionDigest(oldBody), NewDigest: promotionDigest([]byte("new")),
				Phase: "published",
			}},
		},
	}
	if err := writePromotionJournal(backupRoot, journal); err != nil {
		t.Fatal(err)
	}
	if err := recoverPromotionTransactionWithPolicy(backupRoot, policy.promotionDestinationAllowed, 0); err != nil {
		t.Fatalf("recover leftover Caddyfile restore: %v", err)
	}
	body, err := os.ReadFile(legacyDest)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string(oldBody) {
		t.Fatalf("restored Caddyfile=%s", body)
	}
}

func TestLegacyCaddyfileDestinationMustStayUnderGeneratedRoot(t *testing.T) {
	policy := testPolicy(t)
	id := "caddy/legacy.Caddyfile"
	destination, err := resolveBelow(policy.GeneratedRoot, filepath.FromSlash(id))
	if err != nil {
		t.Fatal(err)
	}
	if !policy.promotionDestinationAllowed(id, destination) {
		t.Fatal("matching leftover Caddyfile destination was rejected")
	}
	if policy.promotionDestinationAllowed(id, filepath.Join(policy.GeneratedRoot, "escape.Caddyfile")) {
		t.Fatal("mismatched leftover Caddyfile destination was accepted")
	}
	if policy.promotionDestinationAllowed("caddy/../escape.Caddyfile", destination) {
		t.Fatal("escaped leftover Caddyfile id was accepted")
	}
	if strings.Contains(destination, "..") {
		t.Fatalf("resolved destination escaped: %s", destination)
	}
}
