package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRestoreTransactionCommittedUsesJournalAndReceipt(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	keyPath := filepath.Join(root, "state.key")
	intendedState, intendedKey := []byte("restored-state"), []byte("restored-key")
	if err := os.WriteFile(statePath, intendedState, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, intendedKey, 0o600); err != nil {
		t.Fatal(err)
	}

	committed, err := RestoreTransactionCommitted(statePath, keyPath, "")
	if err != nil || committed {
		t.Fatalf("empty helper state committed=%v err=%v", committed, err)
	}

	writeTestRestoreJournal(t, root, "prepared", checksumHex(intendedState), checksumHex([]byte("other-key")))
	committed, err = RestoreTransactionCommitted(statePath, keyPath, "")
	if err != nil || committed {
		t.Fatalf("uncommitted journal committed=%v err=%v", committed, err)
	}

	writeTestRestoreJournal(t, root, "prepared", checksumHex(intendedState), checksumHex(intendedKey))
	committed, err = RestoreTransactionCommitted(statePath, keyPath, "")
	if err != nil || !committed {
		t.Fatalf("intended targets committed=%v err=%v", committed, err)
	}

	if err := os.Remove(filepath.Join(root, restoreTransactionJournalName)); err != nil {
		t.Fatal(err)
	}
	if err := writeRestoreCommitReceipt(root); err != nil {
		t.Fatal(err)
	}
	committed, err = RestoreTransactionCommitted(statePath, keyPath, "")
	if err != nil || !committed {
		t.Fatalf("receipt after journal unlink committed=%v err=%v", committed, err)
	}
	if err := ClearRestoreCommitReceipt(root); err != nil {
		t.Fatal(err)
	}
	committed, err = RestoreTransactionCommitted(statePath, keyPath, "")
	if err != nil || committed {
		t.Fatalf("cleared receipt committed=%v err=%v", committed, err)
	}
}

func writeTestRestoreJournal(t *testing.T, root, phase, stateDigest, keyDigest string) {
	t.Helper()
	body, err := json.Marshal(restoreJournalDisk{
		Version: 2, TransactionID: "tx-test", Phase: phase,
		Files: []restoreJournalDiskFile{
			{Name: "state.json", TargetID: "state.json", StagedName: ".restore-state-new", SafetyName: "state.json.pre-restore-test", HadPrevious: true, IntendedDigest: stateDigest, Mode: 0o600, Phase: phase},
			{Name: "state.key", TargetID: "state.key", StagedName: ".restore-key-new", SafetyName: "state.key.pre-restore-test", HadPrevious: true, IntendedDigest: keyDigest, Mode: 0o600, Phase: phase},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, restoreTransactionJournalName), body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func checksumHex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
