//go:build unix

package privileged

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Regression for #1257 F3: journal recovery and backup restore used to
// recreate a symlink at a managed destination from a record's wasSymlink /
// oldLinkTarget metadata. OldLinkTarget was validated against nothing but its
// own digest, which the same record carries, so a forged journal or manifest
// made root plant a link to an arbitrary target that normal promote refuses
// to publish over. Managed destinations are never helper-published symlinks,
// so neither path may create one: a wasSymlink record restores "the link was
// removed" and leaves the destination absent.
const symlinkRecordForgedTarget = "/etc/shadow"

func assertNoPromotionDestination(t *testing.T, destination string) {
	t.Helper()
	info, err := os.Lstat(destination)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			target, _ := os.Readlink(destination)
			t.Fatalf("%s was recreated as a symlink to %q", destination, target)
		}
		t.Fatalf("%s still exists (mode %v), want it absent", destination, info.Mode())
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lstat %s: %v", destination, err)
	}
}

func forgedSymlinkRecord(id, destination, safety string) promotionManifestRecord {
	return promotionManifestRecord{
		ArtifactID: id, Destination: destination, BackupPath: safety, SafetyPath: safety,
		HadPrevious: true, WasSymlink: true, OldLinkTarget: symlinkRecordForgedTarget,
		OldDigest: promotionDigest([]byte(symlinkRecordForgedTarget)), Operation: "remove",
	}
}

func TestPromotionRecoveryDoesNotRecreateSymlinkFromJournal(t *testing.T) {
	withStubbedArtifactOwnership(t, func() {
		root := t.TempDir()
		backupRoot := filepath.Join(root, "backups")
		destination := filepath.Join(root, "live", "config-1.json")
		backupID := fixedPromotionBackupID()
		safety := filepath.Join(backupRoot, backupID, "mieru", "config-1.json")
		writeFileWithParents(t, safety, []byte(symlinkRecordForgedTarget))
		writeFileWithParents(t, destination, []byte("live-1"))
		record := forgedSymlinkRecord("mieru/config-1.json", destination, safety)
		record.TransactionID, record.Phase = "forged", "published"
		stagePromotionJournal(t, backupRoot, promotionTransactionJournal{
			Version: 1, TransactionID: "forged", Kind: "promotion", Phase: "artifact-1-published",
			ManifestPath: filepath.Join(backupRoot, backupID, "manifest.json"),
			Manifest: promotionManifest{
				Version: 1, TransactionID: "forged", BackupID: backupID, Kind: "promotion", Phase: "prepared",
				Records: []promotionManifestRecord{record},
			},
		})

		if err := recoverPromotionTransactionWithPolicy(backupRoot, allowPromotionDestinationsUnder(root), 0); err != nil {
			t.Fatalf("recover: %v", err)
		}
		assertNoPromotionDestination(t, destination)
		if _, err := os.Stat(filepath.Join(backupRoot, promotionTransactionJournalName)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("journal after recovery: %v", err)
		}
	})
}

func TestPromotionRestoreDoesNotRecreateSymlinkFromManifest(t *testing.T) {
	withStubbedArtifactOwnership(t, func() {
		root := t.TempDir()
		backupRoot := filepath.Join(root, "backups")
		destination := filepath.Join(root, "live", "config-1.json")
		safety := filepath.Join(backupRoot, lockTestRestoreBackupID, "mieru", "config-1.json")
		writeFileWithParents(t, safety, []byte(symlinkRecordForgedTarget))
		writeFileWithParents(t, destination, []byte("live-1"))
		record := forgedSymlinkRecord("mieru/config-1.json", destination, safety)
		record.Phase = "committed"
		body, err := json.Marshal(promotionManifest{
			Version: 1, BackupID: lockTestRestoreBackupID, Kind: "promotion", Phase: "committed",
			Records: []promotionManifestRecord{record},
		})
		if err != nil {
			t.Fatal(err)
		}
		writeFileWithParents(t, filepath.Join(backupRoot, lockTestRestoreBackupID, "manifest.json"), body)

		result, err := promoteResolvedArtifacts(backupRoot, fixedPromotionNow, ResolvedPromotion{
			ValidateDestination: allowPromotionDestinationsUnder(root), RestoreBackupID: lockTestRestoreBackupID,
		})
		if err != nil {
			t.Fatalf("restore: %v", err)
		}
		assertNoPromotionDestination(t, destination)
		if !slices.Contains(result.RemovedArtifacts, "mieru/config-1.json") || slices.Contains(result.WrittenArtifacts, "mieru/config-1.json") {
			t.Fatalf("restore result written=%v removed=%v, want the symlink record reported as removed",
				result.WrittenArtifacts, result.RemovedArtifacts)
		}
	})
}

// The helper's own record of a removed symlink (a link planted at a managed
// destination before a removal promote) round-trips through rollback without
// the link coming back.
func TestPromotionRollbackOfRemovedSymlinkLeavesDestinationAbsent(t *testing.T) {
	withStubbedArtifactOwnership(t, func() {
		root := t.TempDir()
		backupRoot := filepath.Join(root, "backups")
		destination := filepath.Join(root, "live", "caddy", "legacy.Caddyfile")
		outside := filepath.Join(root, "outside-secret")
		writeFileWithParents(t, outside, []byte("secret"))
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, destination); err != nil {
			t.Fatal(err)
		}
		validate := allowPromotionDestinationsUnder(root)
		removed, err := promoteResolvedArtifacts(backupRoot, fixedPromotionNow, ResolvedPromotion{
			ValidateDestination: validate,
			RemoveArtifacts:     []ResolvedArtifact{{ID: "caddy/legacy.Caddyfile", Destination: destination}},
		})
		if err != nil {
			t.Fatalf("removal promote: %v", err)
		}
		assertNoPromotionDestination(t, destination)
		if removed.BackupID == "" {
			t.Fatal("removal promote returned no backup id")
		}

		restored, err := promoteResolvedArtifacts(backupRoot, fixedPromotionNow, ResolvedPromotion{
			ValidateDestination: validate, RestoreBackupID: removed.BackupID,
		})
		if err != nil {
			t.Fatalf("rollback: %v", err)
		}
		assertNoPromotionDestination(t, destination)
		if slices.Contains(restored.WrittenArtifacts, "caddy/legacy.Caddyfile") {
			t.Fatalf("rollback reported the removed symlink as written: %v", restored.WrittenArtifacts)
		}
		assertFileContent(t, outside, "secret")
	})
}

// verifyCommittedPromotion keeps a read-only "symlink" case so a committed
// journal left by a binary that still published symlinks on restore verifies
// and is retired without touching the link; a link that no longer matches
// the committed digest still fails closed.
func TestPromotionRecoveryVerifiesLegacyCommittedSymlinkJournal(t *testing.T) {
	for _, tamper := range []bool{false, true} {
		name := "matching_link"
		if tamper {
			name = "retargeted_link"
		}
		t.Run(name, func(t *testing.T) {
			withStubbedArtifactOwnership(t, func() {
				root := t.TempDir()
				backupRoot := filepath.Join(root, "backups")
				destination := filepath.Join(root, "live", "config-1.json")
				if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
					t.Fatal(err)
				}
				const legacyTarget = "config-1.real.json"
				linked := legacyTarget
				if tamper {
					linked = "elsewhere.json"
				}
				if err := os.Symlink(linked, destination); err != nil {
					t.Fatal(err)
				}
				backupID := fixedPromotionBackupID()
				manifest := promotionManifest{
					Version: 1, TransactionID: "legacy", BackupID: backupID, Kind: "rollback", Phase: "committed",
					Records: []promotionManifestRecord{{
						TransactionID: "legacy", ArtifactID: "mieru/config-1.json", Destination: destination,
						NewDigest: promotionDigest([]byte(legacyTarget)), Operation: "symlink", Phase: "committed",
					}},
				}
				manifestPath := filepath.Join(backupRoot, backupID, "manifest.json")
				body, err := json.Marshal(manifest)
				if err != nil {
					t.Fatal(err)
				}
				writeFileWithParents(t, manifestPath, body)
				stagePromotionJournal(t, backupRoot, promotionTransactionJournal{
					Version: 1, TransactionID: "legacy", Kind: "rollback", Phase: "manifest-committed",
					ManifestPath: manifestPath, Manifest: manifest,
				})

				err = recoverPromotionTransactionWithPolicy(backupRoot, allowPromotionDestinationsUnder(root), 0)
				journalErr := func() error {
					_, statErr := os.Stat(filepath.Join(backupRoot, promotionTransactionJournalName))
					return statErr
				}()
				if tamper {
					if err == nil {
						t.Fatal("recovery accepted a committed symlink journal whose link was retargeted")
					}
					if journalErr != nil {
						t.Fatalf("recovery retired a journal it could not verify: %v", journalErr)
					}
				} else {
					if err != nil {
						t.Fatalf("recover legacy committed symlink journal: %v", err)
					}
					if !errors.Is(journalErr, os.ErrNotExist) {
						t.Fatalf("journal after recovery: %v", journalErr)
					}
				}
				if got, err := os.Readlink(destination); err != nil || got != linked {
					t.Fatalf("recovery touched the committed link: target=%q err=%v, want %q", got, err, linked)
				}
			})
		})
	}
}
