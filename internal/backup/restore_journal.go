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

var (
	restoreJournalRemove = os.Remove
	errRestoreCommitted  = errors.New("restore committed; journal finalization pending")
)

type restoreTransactionJournal struct {
	Version          int                  `json:"version"`
	TransactionID    string               `json:"transactionId"`
	Phase            string               `json:"phase"`
	PreviousRevision uint64               `json:"previousRevision"`
	IntendedRevision uint64               `json:"intendedRevision"`
	WALCleanupPhase  string               `json:"walShmCleanupPhase"`
	Files            []restoreJournalFile `json:"files"`
	FenceGeneration  uint64               `json:"fenceGeneration"`
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

type restoreJournalDisk struct {
	Version          int                      `json:"version"`
	TransactionID    string                   `json:"transactionId"`
	Phase            string                   `json:"phase"`
	PreviousRevision uint64                   `json:"previousRevision"`
	IntendedRevision uint64                   `json:"intendedRevision"`
	WALCleanupPhase  string                   `json:"walShmCleanupPhase"`
	Files            []restoreJournalDiskFile `json:"files"`
	FenceGeneration  uint64                   `json:"fenceGeneration"`
}

type restoreJournalDiskFile struct {
	Name           string `json:"name"`
	TargetID       string `json:"targetId"`
	StagedName     string `json:"stagedName"`
	SafetyName     string `json:"safetyName"`
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
	expected := map[string]string{"state.json": filepath.Clean(statePath), "state.key": filepath.Clean(keyPath)}
	if databasePath != "" {
		expected["veil.db"] = filepath.Clean(databasePath)
	}
	journal, err := decodeRestoreJournal(body, expected)
	if err != nil {
		return err
	}
	if journal.Version != 2 || journal.TransactionID == "" || len(journal.Files) < 2 {
		return errors.New("invalid restore transaction journal")
	}
	if err := validateRestoreJournalMembers(journal.Files); err != nil {
		return err
	}
	intended, err := restoreJournalTargetsMatch(journal.Files, true)
	if err != nil {
		return err
	}
	if journal.Phase == "committed" || intended {
		if err := cleanupRestoreDatabaseSidecars(root, databasePath, &journal); err != nil {
			return err
		}
		if err := verifyRestoreRevisionBinding(databasePath, journal.IntendedRevision, restoreJournalDigest(journal.Files, "state.json", true), journal.FenceGeneration > 0); err != nil {
			return err
		}
		if err := ensureRestoreFencingFloor(databasePath, journal.FenceGeneration); err != nil {
			return err
		}
		if err := refreshRestoreDatabaseDigest(&journal, databasePath); err != nil {
			return err
		}
		journal.Phase = "committed"
		journal.WALCleanupPhase = "committed"
		for i := range journal.Files {
			journal.Files[i].Phase = "committed"
		}
		if err := writeRestoreJournal(root, journal); err != nil {
			return err
		}
		return removeRestoreJournal(root)
	}
	return rollbackRestoreJournal(root, &journal)
}

func prepareRestoreJournal(statePath string, staged []*stagedRestoreFile, names []string, previousRevision, intendedRevision uint64) (restoreTransactionJournal, error) {
	return prepareRestoreJournalFenced(statePath, staged, names, previousRevision, intendedRevision, 0)
}

func prepareRestoreJournalFenced(statePath string, staged []*stagedRestoreFile, names []string, previousRevision, intendedRevision, fenceGeneration uint64) (restoreTransactionJournal, error) {
	if len(staged) != len(names) {
		return restoreTransactionJournal{}, errors.New("restore journal member mismatch")
	}
	expectedNames := []string{"state.json", "state.key"}
	if len(staged) == 3 {
		expectedNames = append(expectedNames, "veil.db")
	}
	if len(staged) != len(expectedNames) {
		return restoreTransactionJournal{}, errors.New("restore journal requires the exact archive member set")
	}
	seenNames := make(map[string]struct{}, len(names))
	for _, name := range names {
		seenNames[name] = struct{}{}
	}
	for _, name := range expectedNames {
		if _, ok := seenNames[name]; !ok || len(seenNames) != len(expectedNames) {
			return restoreTransactionJournal{}, errors.New("restore journal requires the exact archive member set")
		}
	}
	journal := restoreTransactionJournal{
		Version: 2, TransactionID: uuid.NewString(), Phase: "prepared",
		PreviousRevision: previousRevision, IntendedRevision: intendedRevision,
		WALCleanupPhase: "pending", Files: make([]restoreJournalFile, 0, len(staged)),
		FenceGeneration: fenceGeneration,
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

func prepareRestoredDatabaseRuntimeUnknown(path string, fencingGeneration uint64) error {
	db, err := storage.OpenExisting(path)
	if err != nil {
		return err
	}
	defer db.Close()
	var journalMode string
	if err := db.QueryRow(`PRAGMA journal_mode=DELETE`).Scan(&journalMode); err != nil {
		return fmt.Errorf("set restored database journal mode: %w", err)
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS runtime_verification (
  id INTEGER PRIMARY KEY CHECK (id=1),
  historical_applied_revision INTEGER NOT NULL DEFAULT 0,
  verified_revision INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL CHECK (status IN ('verified','unknown','recovering')),
  updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
)`); err != nil {
		return err
	}
	var historical uint64
	if err := tx.QueryRow(`SELECT applied_revision FROM revisions WHERE id=1`).Scan(&historical); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO runtime_verification(id,historical_applied_revision,verified_revision,status,updated_at)
VALUES(1,?,0,'unknown',strftime('%s','now'))
ON CONFLICT(id) DO UPDATE SET historical_applied_revision=excluded.historical_applied_revision,
 verified_revision=0,status='unknown',updated_at=excluded.updated_at`, historical); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE revisions SET applied_revision=0 WHERE id=1`); err != nil {
		return err
	}
	if fencingGeneration > 0 {
		if _, err := tx.Exec(`UPDATE apply_lease SET generation=MAX(generation,?),owner_process='',current_operation='',heartbeat_at=0,lease_expires_at=0 WHERE id=1`, fencingGeneration); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if err := db.Close(); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(path + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := syncRestoreParent(path + suffix); err != nil {
			return err
		}
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func cleanupRestoreDatabaseSidecars(root, databasePath string, journal *restoreTransactionJournal) error {
	if databasePath == "" {
		return nil
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		path := databasePath + suffix
		if err := restoreRemove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		// Sync even when the sidecar was already absent. This durably orders the
		// absence check before database validation and journal finalization.
		if err := syncRestoreParent(path); err != nil {
			return err
		}
		journal.WALCleanupPhase = suffix[1:] + "-removed"
		journal.Phase = "database-sidecar-" + journal.WALCleanupPhase
		if err := writeRestoreJournal(root, *journal); err != nil {
			return err
		}
	}
	return nil
}

func completeRestoreJournal(root, databasePath string, journal *restoreTransactionJournal) error {
	if err := cleanupRestoreDatabaseSidecars(root, databasePath, journal); err != nil {
		return err
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
	if err := verifyRestoreRevisionBinding(databasePath, journal.IntendedRevision, restoreJournalDigest(journal.Files, "state.json", true), journal.FenceGeneration > 0); err != nil {
		return err
	}
	if err := ensureRestoreFencingFloor(databasePath, journal.FenceGeneration); err != nil {
		return err
	}
	if err := refreshRestoreDatabaseDigest(journal, databasePath); err != nil {
		return err
	}
	journal.Phase = "committed"
	journal.WALCleanupPhase = "committed"
	if err := writeRestoreJournal(root, *journal); err != nil {
		return err
	}
	if err := removeRestoreJournal(root); err != nil {
		return fmt.Errorf("%w: %v", errRestoreCommitted, err)
	}
	return nil
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
	disk := restoreJournalDisk{
		Version: journal.Version, TransactionID: journal.TransactionID, Phase: journal.Phase,
		PreviousRevision: journal.PreviousRevision, IntendedRevision: journal.IntendedRevision,
		WALCleanupPhase: journal.WALCleanupPhase, Files: make([]restoreJournalDiskFile, 0, len(journal.Files)),
		FenceGeneration: journal.FenceGeneration,
	}
	for _, file := range journal.Files {
		if filepath.Dir(file.StagedPath) != filepath.Dir(file.TargetPath) || filepath.Dir(file.SafetyPath) != filepath.Dir(file.TargetPath) {
			return fmt.Errorf("restore journal member %s is outside target directory", file.Name)
		}
		disk.Files = append(disk.Files, restoreJournalDiskFile{
			Name: file.Name, TargetID: file.Name, StagedName: filepath.Base(file.StagedPath), SafetyName: filepath.Base(file.SafetyPath),
			HadPrevious: file.HadPrevious, PreviousDigest: file.PreviousDigest, IntendedDigest: file.IntendedDigest,
			Mode: file.Mode, UID: file.UID, GID: file.GID, Phase: file.Phase,
		})
	}
	body, err := json.Marshal(disk)
	if err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(root, restoreTransactionJournalName), body, 0o600, 0o700)
}

func decodeRestoreJournal(body []byte, expected map[string]string) (restoreTransactionJournal, error) {
	var disk restoreJournalDisk
	if err := json.Unmarshal(body, &disk); err != nil {
		return restoreTransactionJournal{}, fmt.Errorf("decode restore transaction journal: %w", err)
	}
	if disk.Version != 2 {
		return restoreTransactionJournal{}, errors.New("unsafe legacy restore journal version")
	}
	journal := restoreTransactionJournal{
		Version: disk.Version, TransactionID: disk.TransactionID, Phase: disk.Phase,
		PreviousRevision: disk.PreviousRevision, IntendedRevision: disk.IntendedRevision,
		WALCleanupPhase: disk.WALCleanupPhase, Files: make([]restoreJournalFile, 0, len(disk.Files)),
		FenceGeneration: disk.FenceGeneration,
	}
	seen := make(map[string]struct{}, len(disk.Files))
	for _, file := range disk.Files {
		if _, duplicate := seen[file.Name]; duplicate {
			return restoreTransactionJournal{}, fmt.Errorf("duplicate restore journal member %s", file.Name)
		}
		seen[file.Name] = struct{}{}
		target, ok := expected[file.Name]
		if !ok || file.TargetID != file.Name {
			return restoreTransactionJournal{}, fmt.Errorf("restore journal target mismatch for %s", file.Name)
		}
		if !safeRestoreLeaf(file.StagedName) || !safeRestoreLeaf(file.SafetyName) || file.StagedName == file.SafetyName {
			return restoreTransactionJournal{}, fmt.Errorf("restore journal has unsafe member names for %s", file.Name)
		}
		directory := filepath.Dir(target)
		journal.Files = append(journal.Files, restoreJournalFile{
			Name: file.Name, TargetPath: target, StagedPath: filepath.Join(directory, file.StagedName), SafetyPath: filepath.Join(directory, file.SafetyName),
			HadPrevious: file.HadPrevious, PreviousDigest: file.PreviousDigest, IntendedDigest: file.IntendedDigest,
			Mode: file.Mode, UID: file.UID, GID: file.GID, Phase: file.Phase,
		})
	}
	if len(seen) != len(expected) {
		for name := range expected {
			if _, ok := seen[name]; !ok {
				return restoreTransactionJournal{}, fmt.Errorf("missing restore journal member %s", name)
			}
		}
		return restoreTransactionJournal{}, errors.New("restore journal member set mismatch")
	}
	return journal, nil
}

func safeRestoreLeaf(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name
}

func validateRestoreJournalMembers(files []restoreJournalFile) error {
	for _, file := range files {
		if filepath.Dir(file.StagedPath) != filepath.Dir(file.TargetPath) || filepath.Dir(file.SafetyPath) != filepath.Dir(file.TargetPath) {
			return fmt.Errorf("restore journal member %s escapes target directory", file.Name)
		}
		for _, path := range []string{file.TargetPath, file.StagedPath, file.SafetyPath} {
			info, err := os.Lstat(path)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("restore journal member is not a regular file: %s", path)
			}
			if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Nlink != 1 {
				return fmt.Errorf("restore journal member has unsafe hard links: %s", path)
			}
		}
	}
	return nil
}

func restoreJournalTargetsMatch(files []restoreJournalFile, intended bool) (bool, error) {
	for _, file := range files {
		digest := file.PreviousDigest
		if intended {
			digest = file.IntendedDigest
		}
		body, err := os.ReadFile(file.TargetPath)
		if errors.Is(err, os.ErrNotExist) {
			if !intended && !file.HadPrevious {
				continue
			}
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if backupChecksum(body) != digest {
			return false, nil
		}
	}
	return true, nil
}

func restoreJournalDigest(files []restoreJournalFile, name string, intended bool) string {
	for _, file := range files {
		if file.Name == name {
			if intended {
				return file.IntendedDigest
			}
			return file.PreviousDigest
		}
	}
	return ""
}

func validateRestoredSQLite(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA quick_check`)
	if err != nil {
		return fmt.Errorf("restore quick_check: %w", err)
	}
	ok := false
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			rows.Close()
			return err
		}
		if result != "ok" {
			rows.Close()
			return errors.New("restore quick_check failed")
		}
		ok = true
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if !ok {
		return errors.New("restore quick_check returned no result")
	}
	var table string
	if err := db.QueryRow(`SELECT "table" FROM pragma_foreign_key_check LIMIT 1`).Scan(&table); err == nil {
		return errors.New("restore foreign_key_check failed")
	} else if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("restore foreign_key_check: %w", err)
	}
	return nil
}

func verifyRestoreRevisionBinding(databasePath string, expectedRevision uint64, stateDigest string, requireBinding bool) error {
	if databasePath == "" {
		return nil
	}
	db, err := storage.OpenExisting(databasePath)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := validateRestoredSQLite(db); err != nil {
		return err
	}
	var revision uint64
	if err := db.QueryRow(`SELECT desired_revision FROM revisions WHERE id=1`).Scan(&revision); err != nil {
		if errors.Is(err, sql.ErrNoRows) && expectedRevision == 0 {
			return nil
		}
		return fmt.Errorf("restore desired revision row: %w", err)
	}
	if revision != expectedRevision {
		return fmt.Errorf("restore revision mismatch: got %d want %d", revision, expectedRevision)
	}
	if revision == 0 {
		return nil
	}
	var bound string
	if err := db.QueryRow(`SELECT state_sha256 FROM revision_snapshots WHERE revision=?`, revision).Scan(&bound); err != nil {
		if errors.Is(err, sql.ErrNoRows) && !requireBinding {
			return nil
		}
		return fmt.Errorf("restore state digest binding: %w", err)
	}
	if bound == "" && !requireBinding {
		return nil
	}
	if bound == "" || bound != stateDigest {
		return errors.New("restore state digest is not bound to the intended revision")
	}
	return nil
}

func refreshRestoreDatabaseDigest(journal *restoreTransactionJournal, databasePath string) error {
	if databasePath == "" {
		return nil
	}
	body, err := os.ReadFile(databasePath)
	if err != nil {
		return err
	}
	for index := range journal.Files {
		if journal.Files[index].Name == "veil.db" {
			journal.Files[index].IntendedDigest = backupChecksum(body)
			return nil
		}
	}
	return errors.New("restore journal is missing veil.db evidence")
}

func ensureRestoreFencingFloor(databasePath string, generation uint64) error {
	if databasePath == "" || generation == 0 {
		return nil
	}
	db, err := storage.OpenExisting(databasePath)
	if err != nil {
		return err
	}
	defer db.Close()
	var journalMode string
	if err := db.QueryRow(`PRAGMA journal_mode=DELETE`).Scan(&journalMode); err != nil {
		return fmt.Errorf("restore apply fencing journal mode: %w", err)
	}
	result, err := db.Exec(`UPDATE apply_lease SET
 generation=CASE WHEN generation<? THEN ? ELSE generation END,
 owner_process='',lease_expires_at=0,heartbeat_at=0,current_operation=''
WHERE id=1`, generation, generation)
	if err != nil {
		return fmt.Errorf("restore apply fencing floor: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return errors.New("restore apply fencing floor row is missing")
	}
	return nil
}

func removeRestoreJournal(root string) error {
	path := filepath.Join(root, restoreTransactionJournalName)
	if err := restoreJournalRemove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
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
