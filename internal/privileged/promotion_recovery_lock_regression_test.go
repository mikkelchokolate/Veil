//go:build unix

package privileged

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Regression for #1257 F2: promoteResolvedArtifacts and
// restorePromotedArtifacts used to recover the transaction journal before
// taking .promotion.lock. While request A held the lock mid-publish, a second
// request B therefore rolled A's journal back — restoring A's safety copies
// over destinations A had just published and deleting A's journal — before
// B ever blocked on the lock. Every entry point must now wait for the lock
// before it reads, replays or rolls back a journal.
func TestPromotionRecoveryWaitsForPromotionLock(t *testing.T) {
	cases := []struct {
		name string
		run  func(backupRoot string, validate func(string, string) bool) error
	}{
		{
			name: "promote",
			run: func(backupRoot string, validate func(string, string) bool) error {
				_, err := promoteResolvedArtifacts(backupRoot, fixedPromotionNow, ResolvedPromotion{ValidateDestination: validate})
				return err
			},
		},
		{
			name: "restore",
			run: func(backupRoot string, validate func(string, string) bool) error {
				_, err := promoteResolvedArtifacts(backupRoot, fixedPromotionNow, ResolvedPromotion{
					ValidateDestination: validate, RestoreBackupID: lockTestRestoreBackupID,
				})
				return err
			},
		},
		{
			name: "recover",
			run: func(backupRoot string, validate func(string, string) bool) error {
				return recoverPromotionTransactionWithPolicy(backupRoot, validate, 0)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withStubbedArtifactOwnership(t, func() {
				assertRecoveryWaitsForLock(t, tc.run)
			})
		})
	}
}

const lockTestRestoreBackupID = "20260605T120000.000000000Z"

func assertRecoveryWaitsForLock(t *testing.T, run func(string, func(string, string) bool) error) {
	t.Helper()
	root := t.TempDir()
	backupRoot := filepath.Join(root, "backups")
	destination := filepath.Join(root, "live", "config-1.json")
	inFlightID := fixedPromotionBackupID()
	safety := filepath.Join(backupRoot, inFlightID, "mieru", "config-1.json")
	writeFileWithParents(t, safety, []byte("old-1"))
	// Request A has published its new bytes and journaled that, and still
	// holds the lock: nothing may roll this back.
	writeFileWithParents(t, destination, []byte("new-1"))
	stagePromotionJournal(t, backupRoot, promotionTransactionJournal{
		Version: 1, TransactionID: "in-flight", Kind: "promotion", Phase: "artifact-1-published",
		ManifestPath: filepath.Join(backupRoot, inFlightID, "manifest.json"),
		Manifest: promotionManifest{
			Version: 1, TransactionID: "in-flight", BackupID: inFlightID, Kind: "promotion", Phase: "prepared",
			Records: []promotionManifestRecord{{
				TransactionID: "in-flight", ArtifactID: "mieru/config-1.json", Destination: destination,
				BackupPath: safety, SafetyPath: safety, HadPrevious: true,
				OldDigest: promotionDigest([]byte("old-1")), NewDigest: promotionDigest([]byte("new-1")),
				Operation: "write", Phase: "published",
			}},
		},
	})
	// An unrelated, empty committed backup for the restore entry point.
	manifestBody, err := json.Marshal(promotionManifest{Version: 1, BackupID: lockTestRestoreBackupID, Phase: "committed"})
	if err != nil {
		t.Fatal(err)
	}
	writeFileWithParents(t, filepath.Join(backupRoot, lockTestRestoreBackupID, "manifest.json"), manifestBody)

	lock, err := os.OpenFile(filepath.Join(backupRoot, ".promotion.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	locked := true
	defer func() {
		if locked {
			_ = unix.Flock(int(lock.Fd()), unix.LOCK_UN)
		}
	}()

	done := make(chan error, 1)
	go func() { done <- run(backupRoot, allowPromotionDestinationsUnder(root)) }()

	select {
	case err := <-done:
		t.Fatalf("request B finished while request A held the promotion lock: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	assertFileContent(t, destination, "new-1")
	journalPath := filepath.Join(backupRoot, promotionTransactionJournalName)
	if _, err := os.Stat(journalPath); err != nil {
		t.Fatalf("request B consumed request A's in-flight journal before taking the lock: %v", err)
	}

	// Request A commits: its journal is retired, then the lock is released.
	if err := os.Remove(journalPath); err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	locked = false
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("request B after the lock was released: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("request B did not proceed after the promotion lock was released")
	}
	assertFileContent(t, destination, "new-1")
	if _, err := os.Stat(journalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("journal after request B: %v", err)
	}
}
