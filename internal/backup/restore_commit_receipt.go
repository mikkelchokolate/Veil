package backup

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
)

const restoreCommitReceiptName = ".veil-restore-committed"

func restoreCommitReceiptPath(root string) string {
	return filepath.Join(root, restoreCommitReceiptName)
}

func writeRestoreCommitReceipt(root string) error {
	return atomicfile.Write(restoreCommitReceiptPath(root), []byte(`{"version":1,"committed":true}`+"\n"), 0o600, 0o700)
}

// ClearRestoreCommitReceipt removes the helper commit receipt after the Panel
// has recorded a terminal restore job status.
func ClearRestoreCommitReceipt(root string) error {
	if root == "" {
		return nil
	}
	if err := os.Remove(restoreCommitReceiptPath(root)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func restoreCommitReceiptPresent(root string) bool {
	info, err := os.Stat(restoreCommitReceiptPath(root))
	return err == nil && info.Mode().IsRegular()
}

// RestoreTransactionCommitted reports whether the helper restore journal has
// already published the intended triple. A missing journal is treated as
// committed only when the durable commit receipt remains after unlink.
func RestoreTransactionCommitted(statePath, keyPath, databasePath string) (bool, error) {
	if statePath == "" {
		return false, nil
	}
	root := filepath.Dir(statePath)
	journalPath := filepath.Join(root, restoreTransactionJournalName)
	body, err := os.ReadFile(journalPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err == nil {
		var disk restoreJournalDisk
		if unmarshalErr := json.Unmarshal(body, &disk); unmarshalErr != nil {
			return false, unmarshalErr
		}
		expected := map[string]string{}
		for _, file := range disk.Files {
			switch file.Name {
			case "state.json":
				expected[file.Name] = filepath.Clean(statePath)
			case "state.key":
				expected[file.Name] = filepath.Clean(keyPath)
			case "veil.db":
				dbPath := databasePath
				if dbPath == "" {
					dbPath = filepath.Join(root, "veil.db")
				}
				expected[file.Name] = filepath.Clean(dbPath)
			}
		}
		journal, decodeErr := decodeRestoreJournal(body, expected)
		if decodeErr != nil {
			return false, decodeErr
		}
		if journal.Version != 2 || journal.TransactionID == "" || len(journal.Files) < 2 {
			return false, errors.New("invalid restore transaction journal")
		}
		intended, matchErr := restoreJournalTargetsMatch(journal.Files, true)
		if matchErr != nil {
			return false, matchErr
		}
		return journal.Phase == "committed" || intended, nil
	}
	return restoreCommitReceiptPresent(root), nil
}
