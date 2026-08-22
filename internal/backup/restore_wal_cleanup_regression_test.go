package backup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/storage"
)

func TestCommittedRestoreRecoveryActuallyCleansWALAndSHMBeforeJournalRemoval(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	keyPath := filepath.Join(root, "state.key")
	databasePath := filepath.Join(root, "veil.db")
	stateBody := []byte(`{"settings":{}}`)
	keyBody := []byte("01234567890123456789012345678901")
	if err := os.WriteFile(statePath, stateBody, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyBody, 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT OR REPLACE INTO revisions(id,desired_revision,applied_revision) VALUES(1,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO revision_snapshots(revision,payload,state_sha256) VALUES(1,'{}',?)`, backupChecksum(stateBody)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	databaseBody, err := os.ReadFile(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	journal := restoreTransactionJournal{
		Version: 2, TransactionID: "txn", Phase: "committed", IntendedRevision: 1,
		WALCleanupPhase: "committed", FenceGeneration: 1,
		Files: []restoreJournalFile{
			{Name: "state.json", TargetPath: statePath, StagedPath: filepath.Join(root, ".state.stage"), SafetyPath: filepath.Join(root, ".state.safety"), IntendedDigest: backupChecksum(stateBody), Phase: "committed"},
			{Name: "state.key", TargetPath: keyPath, StagedPath: filepath.Join(root, ".key.stage"), SafetyPath: filepath.Join(root, ".key.safety"), IntendedDigest: backupChecksum(keyBody), Phase: "committed"},
			{Name: "veil.db", TargetPath: databasePath, StagedPath: filepath.Join(root, ".db.stage"), SafetyPath: filepath.Join(root, ".db.safety"), IntendedDigest: backupChecksum(databaseBody), Phase: "committed"},
		},
	}
	if err := writeRestoreJournal(root, journal); err != nil {
		t.Fatal(err)
	}
	oldRemove := restoreRemove
	defer func() { restoreRemove = oldRemove }()
	removed := map[string]bool{}
	restoreRemove = func(path string) error {
		removed[path] = true
		return os.Remove(path)
	}
	if err := RecoverInterruptedRestore(statePath, keyPath, databasePath); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if !removed[databasePath+suffix] {
			t.Fatalf("committed recovery skipped %s cleanup", suffix)
		}
	}
}
