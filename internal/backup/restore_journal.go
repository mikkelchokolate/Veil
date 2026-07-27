package backup

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/google/uuid"
	"github.com/mikkelchokolate/Veil/internal/atomicfile"
	"github.com/mikkelchokolate/Veil/internal/storage"
)

const restoreTransactionJournalName = ".veil-restore-journal.json"

type restoreTransactionJournal struct {
	Version          int                  `json:"version"`
	TransactionID    string               `json:"transactionId"`
	Phase            string               `json:"phase"`
	PreviousRevision uint64               `json:"previousRevision"`
	IntendedRevision uint64               `json:"intendedRevision"`
	WALCleanupPhase  string               `json:"walShmCleanupPhase"`
	Files            []restoreJournalFile `json:"files"`
}

type restoreJournalFile struct {
	Name           string `json:"name"`
	TargetPath     string `json:"targetPath"`
	StagedPath     string `json:"stagedPath"`
	SafetyPath     string `json:"safetyPath"`
	HadPrevious    bool   `json:"hadPrevious"`
	PreviousDigest string `json:"previousDigest,omitempty"`
	IntendedDigest string `json:"intendedDigest"`
	Mode           uint32 `json:"mode"`
	UID            int    `json:"uid"`
	GID            int    `json:"gid"`
	Phase          string `json:"phase"`
}

// RecoverInterruptedRestore is safe to call before opening state.key or veil.db.
// A surviving journal means the helper never durably completed the restore, so
// recovery deterministically restores the exact checkpointed old triple.
func RecoverInterruptedRestore(statePath, keyPath, databasePath string) error {
	if statePath == "" {
		return nil
	}
	root := filepath.Dir(statePath)
	journalPath := filepath.Join(root, restoreTransactionJournalName)
	body, err := os.ReadFile(journalPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var journal restoreTransactionJournal
	if err := json.Unmarshal(body, &journal); err != nil {
		return fmt.Errorf("decode restore transaction journal: %w", err)
	}
	if journal.Version != 1 || journal.TransactionID == "" || len(journal.Files) < 2 {
		return errors.New("invalid restore transaction journal")
	}
	expected := map[string]string{"state.json": filepath.Clean(statePath), "state.key": filepath.Clean(keyPath)}
	if databasePath != "" {
		expected["veil.db"] = filepath.Clean(databasePath)
	}
	for _, file := range journal.Files {
		if target, ok := expected[file.Name]; !ok || filepath.Clean(file.TargetPath) != target {
			return fmt.Errorf("restore journal target mismatch for %s", file.Name)
		}
	}
	return rollbackRestoreJournal(root, &journal)
}

func prepareRestoreJournal(statePath string, staged []*stagedRestoreFile, names []string, previousRevision, intendedRevision uint64) (restoreTransactionJournal, error) {
	if len(staged) != len(names) {
		return restoreTransactionJournal{}, errors.New("restore journal member mismatch")
	}
	journal := restoreTransactionJournal{
		Version: 1, TransactionID: uuid.NewString(), Phase: "prepared",
		PreviousRevision: previousRevision, IntendedRevision: intendedRevision,
		WALCleanupPhase: "pending", Files: make([]restoreJournalFile, 0, len(staged)),
	}
	for index, item := range staged {
		record := restoreJournalFile{
			Name: names[index], TargetPath: item.target, StagedPath: item.temp,
			SafetyPath: item.safety, HadPrevious: item.hadOriginal, Phase: "prepared",
			Mode: 0o600, UID: os.Getuid(), GID: os.Getgid(),
		}
		intended, err := os.ReadFile(item.temp)
		if err != nil {
			return restoreTransactionJournal{}, err
		}
		record.IntendedDigest = backupChecksum(intended)
		if item.hadOriginal {
			previous, err := os.ReadFile(item.target)
			if err != nil {
				return restoreTransactionJournal{}, err
			}
			record.PreviousDigest = backupChecksum(previous)
			info, err := os.Stat(item.target)
			if err != nil {
				return restoreTransactionJournal{}, err
			}
			record.Mode = uint32(info.Mode().Perm())
			if stat, ok := info.Sys().(*syscall.Stat_t); ok {
				record.UID, record.GID = int(stat.Uid), int(stat.Gid)
			}
		}
		journal.Files = append(journal.Files, record)
	}
	if err := writeRestoreJournal(filepath.Dir(statePath), journal); err != nil {
		return restoreTransactionJournal{}, err
	}
	return journal, nil
}

func publishRestoreJournalFile(root string, journal *restoreTransactionJournal, index int) error {
	record := &journal.Files[index]
	if record.HadPrevious {
		if err := restoreRename(record.TargetPath, record.SafetyPath); err != nil {
			return err
		}
		if err := syncRestoreParent(record.TargetPath); err != nil {
			return err
		}
		record.Phase = "safety-published"
		journal.Phase = record.Name + "-safety-published"
		if err := writeRestoreJournal(root, *journal); err != nil {
			return err
		}
	}
	if err := restoreRename(record.StagedPath, record.TargetPath); err != nil {
		return err
	}
	if err := syncRestoreParent(record.TargetPath); err != nil {
		return err
	}
	body, err := os.ReadFile(record.TargetPath)
	if err != nil {
		return err
	}
	if backupChecksum(body) != record.IntendedDigest {
		return fmt.Errorf("restore intended digest mismatch for %s", record.Name)
	}
	record.Phase = "intended-published"
	journal.Phase = record.Name + "-intended-published"
	return writeRestoreJournal(root, *journal)
}

func completeRestoreJournal(root, databasePath string, journal *restoreTransactionJournal) error {
	if databasePath != "" {
		for _, suffix := range []string{"-wal", "-shm"} {
			path := databasePath + suffix
			if err := restoreRemove(path); err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					return err
				}
			} else if err := syncRestoreParent(path); err != nil {
				return err
			}
			journal.WALCleanupPhase = suffix[1:] + "-removed"
			journal.Phase = "database-sidecar-" + journal.WALCleanupPhase
			if err := writeRestoreJournal(root, *journal); err != nil {
				return err
			}
		}
	}
	for index := range journal.Files {
		body, err := os.ReadFile(journal.Files[index].TargetPath)
		if err != nil {
			return err
		}
		if backupChecksum(body) != journal.Files[index].IntendedDigest {
			return fmt.Errorf("restore final digest mismatch for %s", journal.Files[index].Name)
		}
		journal.Files[index].Phase = "committed"
	}
	journal.Phase = "committed"
	journal.WALCleanupPhase = "committed"
	if err := writeRestoreJournal(root, *journal); err != nil {
		return err
	}
	return removeRestoreJournal(root)
}

func rollbackRestoreJournal(root string, journal *restoreTransactionJournal) error {
	journal.Phase = "rolling-back"
	_ = writeRestoreJournal(root, *journal)
	for index := len(journal.Files) - 1; index >= 0; index-- {
		record := &journal.Files[index]
		if record.HadPrevious {
			if _, err := os.Stat(record.SafetyPath); err == nil {
				if err := restoreRename(record.SafetyPath, record.TargetPath); err != nil {
					return err
				}
				if err := syncRestoreParent(record.TargetPath); err != nil {
					return err
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			body, err := os.ReadFile(record.TargetPath)
			if err != nil {
				return fmt.Errorf("recover previous %s: %w", record.Name, err)
			}
			if backupChecksum(body) != record.PreviousDigest {
				return fmt.Errorf("recover previous digest mismatch for %s", record.Name)
			}
			if err := os.Chmod(record.TargetPath, os.FileMode(record.Mode)); err != nil {
				return err
			}
			if os.Geteuid() == 0 {
				if err := os.Chown(record.TargetPath, record.UID, record.GID); err != nil {
					return err
				}
			}
		} else {
			if err := restoreRemove(record.TargetPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if err := syncRestoreParent(record.TargetPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		if err := restoreRemove(record.StagedPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		record.Phase = "rolled-back"
		journal.Phase = record.Name + "-rolled-back"
		if err := writeRestoreJournal(root, *journal); err != nil {
			return err
		}
	}
	for _, file := range journal.Files {
		if file.Name == "veil.db" {
			for _, suffix := range []string{"-wal", "-shm"} {
				if err := restoreRemove(file.TargetPath + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
		}
	}
	journal.Phase = "rolled-back"
	if err := writeRestoreJournal(root, *journal); err != nil {
		return err
	}
	return removeRestoreJournal(root)
}

func writeRestoreJournal(root string, journal restoreTransactionJournal) error {
	body, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(root, restoreTransactionJournalName), body, 0o600, 0o700)
}

func removeRestoreJournal(root string) error {
	path := filepath.Join(root, restoreTransactionJournalName)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncRestoreParent(path)
}

func syncRestoreParent(path string) error {
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func readRestoreRevision(databasePath string) (uint64, error) {
	if databasePath == "" {
		return 0, nil
	}
	if _, err := os.Stat(databasePath); errors.Is(err, os.ErrNotExist) {
		return 0, nil
	} else if err != nil {
		return 0, err
	}
	db, err := storage.OpenExisting(databasePath)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var revision uint64
	if err := db.QueryRow(`SELECT desired_revision FROM revisions WHERE id=1`).Scan(&revision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	return revision, nil
}
