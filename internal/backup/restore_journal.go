package backup

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mikkelchokolate/Veil/internal/atomicfile"
	"github.com/mikkelchokolate/Veil/internal/safefs"
	"github.com/mikkelchokolate/Veil/internal/storage"
)

const restoreTransactionJournalName = ".veil-restore-journal.json"

// restoreJournalMaxBytes bounds the journal read: the file lives in the
// service-writable state root so an unbounded read would let a planted
// multi-GB journal exhaust memory before it could even be validated (#1219).
const restoreJournalMaxBytes = 4 << 20

var (
	// restoreJournalRemove unlinks the journal leaf relative to its pinned
	// root directory: (dir, leaf) — never a re-resolved path (#1219).
	restoreJournalRemove = func(dir *safefs.Dir, leaf string) error {
		return dir.RemoveAt(leaf)
	}
	errRestoreCommitted = errors.New("restore committed; journal finalization pending")
)

// restoreJournalDirs caches one pinned *safefs.Dir per member directory for
// the length of a journal operation, so every stat/open/rename/remove runs
// descriptor-relative and a directory swapped mid-recovery cannot redirect
// the operation (#1219).
type restoreJournalDirs map[string]*safefs.Dir

func (c restoreJournalDirs) forTarget(targetPath string) (*safefs.Dir, error) {
	dirPath := filepath.Dir(targetPath)
	if dir, ok := c[dirPath]; ok {
		return dir, nil
	}
	dir, err := safefs.OpenDir(dirPath)
	if err != nil {
		return nil, err
	}
	c[dirPath] = dir
	return dir, nil
}

func (c restoreJournalDirs) Close() {
	for _, dir := range c {
		_ = dir.Close()
	}
}

// digestJournalLeaf hashes a leaf inside a pinned directory; the leaf is
// opened O_NOFOLLOW relative to the directory descriptor, so a swapped
// symlink is rejected rather than followed (#1219).
func digestJournalLeaf(dir *safefs.Dir, leaf string) (string, error) {
	file, err := dir.OpenFileAt(leaf)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// quarantineRestoreJournal renames a journal that failed validation or could
// not finish its own recovery to a non-residue name. The failure is reported
// once; the renamed journal is never replayed, so a malformed or
// self-defeating journal cannot wedge startup into an endless recovery loop.
// The quarantine name deliberately avoids the ".veil-restore-" residue
// prefix so the crash sweeper does not garbage-collect the evidence (#1219).
func quarantineRestoreJournal(rootDir *safefs.Dir) error {
	quarantine := fmt.Sprintf("veil-restore-journal.failed-%d", time.Now().UnixNano())
	if err := rootDir.RenameAt(restoreTransactionJournalName, quarantine); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return rootDir.File().Sync()
}

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
	rootPath := filepath.Dir(statePath)
	// Pin the state root once: the journal and every staged/safety/member
	// operation below resolve relative to this descriptor, so an attacker who
	// owns the directory can rename entries but can never redirect an
	// operation outside the pinned inode (#1219).
	rootDir, err := safefs.OpenDir(rootPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer rootDir.Close()
	journalFile, err := rootDir.OpenFileAt(restoreTransactionJournalName)
	if errors.Is(err, os.ErrNotExist) {
		// No live journal means no interrupted restore can still reference
		// staged temps — sweep crash leftovers (.veil-restore-*, atomicfile
		// .tmp-*, backup db snapshots) before proceeding (#1125).
		sweepInterruptedRestoreResidue(rootDir)
		return nil
	}
	if err != nil {
		return err
	}
	body, readErr := io.ReadAll(io.LimitReader(journalFile, restoreJournalMaxBytes))
	_ = journalFile.Close()
	if readErr != nil {
		return readErr
	}
	// The journal is attacker-influenced: anything that fails validation — or
	// fails its own recovery — is quarantined once and reported, never
	// replayed, so a malformed journal cannot wedge recovery forever (#1219).
	fail := func(cause error) error {
		if quarantineErr := quarantineRestoreJournal(rootDir); quarantineErr != nil {
			return fmt.Errorf("%w (restore journal quarantine failed: %v)", cause, quarantineErr)
		}
		return cause
	}
	var disk restoreJournalDisk
	if err := json.Unmarshal(body, &disk); err != nil {
		return fail(fmt.Errorf("decode restore transaction journal: %w", err))
	}
	// The expected member set is derived from the journal's own member names:
	// a legacy restore journal legitimately carries only state.json+state.key
	// even when a live database exists at databasePath, so keying expected
	// members off databasePath != "" would reject such journals forever and
	// wedge recovery on "missing restore journal member veil.db" (#1116).
	journal, err := decodeRestoreJournal(body, restoreJournalExpectedTargets(disk, statePath, keyPath, databasePath))
	if err != nil {
		return fail(err)
	}
	if journal.Version != 2 || journal.TransactionID == "" || len(journal.Files) < 2 {
		return fail(errors.New("invalid restore transaction journal"))
	}
	dirs := restoreJournalDirs{}
	defer dirs.Close()
	if err := validateRestoreJournalMembers(dirs, journal.Files); err != nil {
		return fail(err)
	}
	intended, err := restoreJournalTargetsMatch(dirs, journal.Files, true)
	if err != nil {
		return fail(err)
	}
	if journal.Phase == "committed" || intended {
		// Database-side recovery is meaningful only when this journal
		// actually staged veil.db: a two-member legacy journal must not
		// delete sidecars or verify a revision binding on a live database
		// the interrupted restore never touched (#1116).
		if journalDatabase := restoreJournalMemberTarget(journal.Files, "veil.db"); journalDatabase != "" {
			if err := cleanupRestoreDatabaseSidecars(dirs, rootPath, journalDatabase, &journal); err != nil {
				return fail(err)
			}
			if err := verifyRestoreRevisionBinding(journalDatabase, journal.IntendedRevision, restoreJournalDigest(journal.Files, "state.json", true), journal.FenceGeneration > 0); err != nil {
				return fail(err)
			}
			if err := ensureRestoreFencingFloor(journalDatabase, journal.FenceGeneration); err != nil {
				return fail(err)
			}
			if err := refreshRestoreDatabaseDigest(dirs, &journal, journalDatabase); err != nil {
				return fail(err)
			}
		}
		journal.Phase = "committed"
		journal.WALCleanupPhase = "committed"
		for i := range journal.Files {
			journal.Files[i].Phase = "committed"
		}
		if err := writeRestoreJournal(rootPath, journal); err != nil {
			return fail(err)
		}
		if err := writeRestoreCommitReceipt(rootPath); err != nil {
			return fail(err)
		}
		return removeRestoreJournal(rootDir)
	}
	if err := rollbackRestoreJournal(dirs, rootPath, &journal); err != nil {
		return fail(err)
	}
	return nil
}

// restoreJournalExpectedTargets maps journal member names to the live targets
// the recoverer was asked to check. Members are keyed off the journal itself:
// unknown member names yield no expected entry so decodeRestoreJournal still
// rejects foreign member sets, while a journal that simply does not carry
// veil.db decodes cleanly instead of reporting a missing member (#1116).
func restoreJournalExpectedTargets(disk restoreJournalDisk, statePath, keyPath, databasePath string) map[string]string {
	expected := make(map[string]string, len(disk.Files))
	for _, file := range disk.Files {
		switch file.Name {
		case "state.json":
			expected[file.Name] = filepath.Clean(statePath)
		case "state.key":
			expected[file.Name] = filepath.Clean(keyPath)
		case "veil.db":
			dbPath := databasePath
			if dbPath == "" {
				dbPath = filepath.Join(filepath.Dir(statePath), "veil.db")
			}
			expected[file.Name] = filepath.Clean(dbPath)
		}
	}
	return expected
}

// restoreJournalMemberTarget returns the journal-recorded target path for a
// member, or "" when the journal does not carry that member.
func restoreJournalMemberTarget(files []restoreJournalFile, name string) string {
	for _, file := range files {
		if file.Name == name {
			return file.TargetPath
		}
	}
	return ""
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
		// Digests, mode and ownership are taken from the descriptor-pinned
		// observation captured at stage time — the journal records what was
		// actually verified, not whatever a swapped leaf resolves to now
		// (#1219).
		record := restoreJournalFile{
			Name: names[index], TargetPath: item.target, StagedPath: item.temp,
			SafetyPath: item.safety, HadPrevious: item.hadOriginal, Phase: "prepared",
			Mode:           uint32(item.mode),
			UID:            item.uid,
			GID:            item.gid,
			IntendedDigest: item.intendedDigest,
		}
		if item.hadOriginal {
			record.PreviousDigest = item.previousDigest
		}
		journal.Files = append(journal.Files, record)
	}
	root := filepath.Dir(statePath)
	if err := ClearRestoreCommitReceipt(root); err != nil {
		return restoreTransactionJournal{}, err
	}
	if err := writeRestoreJournal(root, journal); err != nil {
		return restoreTransactionJournal{}, err
	}
	return journal, nil
}

func publishRestoreJournalFile(dirs restoreJournalDirs, root string, journal *restoreTransactionJournal, index int) error {
	record := &journal.Files[index]
	// All member mutations are descriptor-relative inside the pinned target
	// directory; nothing here follows a re-resolved path (#1219).
	dir, err := dirs.forTarget(record.TargetPath)
	if err != nil {
		return err
	}
	targetLeaf := filepath.Base(record.TargetPath)
	if record.HadPrevious {
		// Bind the safety swap to the inode that was actually validated at
		// stage time: if the live target's digest no longer matches the
		// journal-recorded PreviousDigest, a swap won a race mid-publish —
		// fail before it is archived as the "previous" copy (#1219).
		digest, err := digestJournalLeaf(dir, targetLeaf)
		if err != nil {
			return fmt.Errorf("verify restore target %s before safety swap: %w", record.Name, err)
		}
		if digest != record.PreviousDigest {
			return fmt.Errorf("restore target %s changed since staging", record.Name)
		}
		if err := restoreRename(dir, targetLeaf, filepath.Base(record.SafetyPath)); err != nil {
			return err
		}
		if err := dir.File().Sync(); err != nil {
			return err
		}
		record.Phase = "safety-published"
		journal.Phase = record.Name + "-safety-published"
		if err := writeRestoreJournal(root, *journal); err != nil {
			return err
		}
	}
	stagedLeaf := filepath.Base(record.StagedPath)
	stagedInfo, err := dir.StatAt(stagedLeaf)
	if err != nil {
		return err
	}
	if !stagedInfo.Mode().IsRegular() {
		return fmt.Errorf("restore staged member is not a regular file: %s", record.Name)
	}
	if unsafeJournalHardLink(stagedInfo) {
		return fmt.Errorf("restore staged member has unsafe hard links: %s", record.Name)
	}
	if err := restoreRename(dir, stagedLeaf, targetLeaf); err != nil {
		return err
	}
	if err := dir.File().Sync(); err != nil {
		return err
	}
	digest, err := digestJournalLeaf(dir, targetLeaf)
	if err != nil {
		return err
	}
	if digest != record.IntendedDigest {
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

func cleanupRestoreDatabaseSidecars(dirs restoreJournalDirs, root, databasePath string, journal *restoreTransactionJournal) error {
	if databasePath == "" {
		return nil
	}
	dir, err := dirs.forTarget(databasePath)
	if err != nil {
		return err
	}
	leaf := filepath.Base(databasePath)
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := restoreRemove(dir, leaf+suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		// Sync even when the sidecar was already absent. This durably orders the
		// absence check before database validation and journal finalization.
		if err := dir.File().Sync(); err != nil {
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
	dirs := restoreJournalDirs{}
	defer dirs.Close()
	if err := cleanupRestoreDatabaseSidecars(dirs, root, databasePath, journal); err != nil {
		return err
	}
	for index := range journal.Files {
		record := &journal.Files[index]
		dir, err := dirs.forTarget(record.TargetPath)
		if err != nil {
			return err
		}
		digest, err := digestJournalLeaf(dir, filepath.Base(record.TargetPath))
		if err != nil {
			return err
		}
		if digest != record.IntendedDigest {
			return fmt.Errorf("restore final digest mismatch for %s", record.Name)
		}
		record.Phase = "committed"
	}
	if err := verifyRestoreRevisionBinding(databasePath, journal.IntendedRevision, restoreJournalDigest(journal.Files, "state.json", true), journal.FenceGeneration > 0); err != nil {
		return err
	}
	if err := ensureRestoreFencingFloor(databasePath, journal.FenceGeneration); err != nil {
		return err
	}
	if err := refreshRestoreDatabaseDigest(dirs, journal, databasePath); err != nil {
		return err
	}
	journal.Phase = "committed"
	journal.WALCleanupPhase = "committed"
	if err := writeRestoreJournal(root, *journal); err != nil {
		return err
	}
	if err := writeRestoreCommitReceipt(root); err != nil {
		return err
	}
	rootDir, err := safefs.OpenDir(root)
	if err != nil {
		return err
	}
	defer rootDir.Close()
	if err := removeRestoreJournal(rootDir); err != nil {
		return fmt.Errorf("%w: %v", errRestoreCommitted, err)
	}
	return nil
}

func rollbackRestoreJournal(dirs restoreJournalDirs, root string, journal *restoreTransactionJournal) error {
	journal.Phase = "rolling-back"
	_ = writeRestoreJournal(root, *journal)
	for index := len(journal.Files) - 1; index >= 0; index-- {
		record := &journal.Files[index]
		dir, err := dirs.forTarget(record.TargetPath)
		if err != nil {
			return err
		}
		targetLeaf := filepath.Base(record.TargetPath)
		if record.HadPrevious {
			safetyLeaf := filepath.Base(record.SafetyPath)
			info, statErr := dir.StatAt(safetyLeaf)
			if statErr == nil {
				// The safety swap must move the inode the journal actually
				// recorded: a swapped symlink or multi-linked entry is
				// rejected here instead of renamed into place (#1219).
				if !info.Mode().IsRegular() {
					return fmt.Errorf("restore safety member is not a regular file: %s", record.Name)
				}
				if unsafeJournalHardLink(info) {
					return fmt.Errorf("restore safety member has unsafe hard links: %s", record.Name)
				}
				if err := restoreRename(dir, safetyLeaf, targetLeaf); err != nil {
					return err
				}
				if err := dir.File().Sync(); err != nil {
					return err
				}
			} else if !errors.Is(statErr, os.ErrNotExist) {
				return statErr
			}
			digest, err := digestJournalLeaf(dir, targetLeaf)
			if err != nil {
				return fmt.Errorf("recover previous %s: %w", record.Name, err)
			}
			if digest != record.PreviousDigest {
				return fmt.Errorf("recover previous digest mismatch for %s", record.Name)
			}
			// No chmod/chown on the restored target: the safety inode kept
			// its original mode and ownership through the rename, so there
			// is no metadata operation left to race against a swapped leaf.
			// Journal Mode/UID/GID are treated as untrusted hints and never
			// applied (#1219).
		} else {
			if err := restoreRemove(dir, targetLeaf); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if err := dir.File().Sync(); err != nil {
				return err
			}
		}
		if err := restoreRemove(dir, filepath.Base(record.StagedPath)); err != nil && !errors.Is(err, os.ErrNotExist) {
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
			dir, err := dirs.forTarget(file.TargetPath)
			if err != nil {
				return err
			}
			leaf := filepath.Base(file.TargetPath)
			for _, suffix := range []string{"-wal", "-shm"} {
				if err := restoreRemove(dir, leaf+suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
		}
	}
	journal.Phase = "rolled-back"
	if err := writeRestoreJournal(root, *journal); err != nil {
		return err
	}
	if err := ClearRestoreCommitReceipt(root); err != nil {
		return err
	}
	// The journal leaf lives in the same directory as the state member —
	// reuse the pinned handle rather than opening a second one.
	rootDir, err := dirs.forTarget(filepath.Join(root, restoreTransactionJournalName))
	if err != nil {
		return err
	}
	return removeRestoreJournal(rootDir)
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

// restoreJournalMemberPhases is the closed set of per-member phases a v2
// journal may claim; anything else is rejected before any mutation (#1219).
var restoreJournalMemberPhases = map[string]bool{
	"prepared":           true,
	"safety-published":   true,
	"intended-published": true,
	"committed":          true,
	"rolled-back":        true,
}

// restoreJournalWALPhases is the closed set of WALCleanupPhase values.
var restoreJournalWALPhases = map[string]bool{
	"pending":     true,
	"wal-removed": true,
	"shm-removed": true,
	"committed":   true,
}

// validRestoreJournalPhase restricts the journal-level Phase field to the
// values recovery itself can produce: fixed phases plus member-scoped
// progress markers derived from the journal's own member names (#1219).
func validRestoreJournalPhase(phase string, memberNames map[string]struct{}) bool {
	switch phase {
	case "prepared", "rolling-back", "rolled-back", "committed",
		"database-sidecar-wal-removed", "database-sidecar-shm-removed":
		return true
	}
	for _, suffix := range []string{"-safety-published", "-intended-published", "-rolled-back"} {
		name := strings.TrimSuffix(phase, suffix)
		if name != phase {
			_, ok := memberNames[name]
			return ok
		}
	}
	return false
}

// isSHA256HexDigest requires digests to be exact sha256 hex strings —
// journals that smuggle empty, truncated or oversized digests must not reach
// the byte-comparison path where a malformed value could alias "".
func isSHA256HexDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
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
		if !restoreJournalMemberPhases[file.Phase] {
			return restoreTransactionJournal{}, fmt.Errorf("restore journal member %s has unsupported phase %q", file.Name, file.Phase)
		}
		if !isSHA256HexDigest(file.IntendedDigest) {
			return restoreTransactionJournal{}, fmt.Errorf("restore journal member %s has invalid intended digest", file.Name)
		}
		if file.HadPrevious != (file.PreviousDigest != "") {
			return restoreTransactionJournal{}, fmt.Errorf("restore journal member %s has inconsistent previous evidence", file.Name)
		}
		if file.HadPrevious && !isSHA256HexDigest(file.PreviousDigest) {
			return restoreTransactionJournal{}, fmt.Errorf("restore journal member %s has invalid previous digest", file.Name)
		}
		directory := filepath.Dir(target)
		// Mode/UID/GID from the journal are deliberately NOT propagated: they
		// are attacker-controlled hints, and rollback never re-applies
		// metadata — the preserved safety inode already carries it (#1219).
		journal.Files = append(journal.Files, restoreJournalFile{
			Name: file.Name, TargetPath: target, StagedPath: filepath.Join(directory, file.StagedName), SafetyPath: filepath.Join(directory, file.SafetyName),
			HadPrevious: file.HadPrevious, PreviousDigest: file.PreviousDigest, IntendedDigest: file.IntendedDigest,
			Phase: file.Phase,
		})
	}
	if !restoreJournalWALPhases[disk.WALCleanupPhase] {
		return restoreTransactionJournal{}, fmt.Errorf("restore journal has unsupported wal cleanup phase %q", disk.WALCleanupPhase)
	}
	if !validRestoreJournalPhase(disk.Phase, seen) {
		return restoreTransactionJournal{}, fmt.Errorf("restore journal has unsupported phase %q", disk.Phase)
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

func validateRestoreJournalMembers(dirs restoreJournalDirs, files []restoreJournalFile) error {
	for _, file := range files {
		if filepath.Dir(file.StagedPath) != filepath.Dir(file.TargetPath) || filepath.Dir(file.SafetyPath) != filepath.Dir(file.TargetPath) {
			return fmt.Errorf("restore journal member %s escapes target directory", file.Name)
		}
		dir, err := dirs.forTarget(file.TargetPath)
		if err != nil {
			return err
		}
		for _, leaf := range []string{filepath.Base(file.TargetPath), filepath.Base(file.StagedPath), filepath.Base(file.SafetyPath)} {
			info, err := dir.StatAt(leaf)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("restore journal member is not a regular file: %s", filepath.Join(dir.Path(), leaf))
			}
			if unsafeJournalHardLink(info) {
				return fmt.Errorf("restore journal member has unsafe hard links: %s", filepath.Join(dir.Path(), leaf))
			}
		}
	}
	return nil
}

func restoreJournalTargetsMatch(dirs restoreJournalDirs, files []restoreJournalFile, intended bool) (bool, error) {
	for _, file := range files {
		digest := file.PreviousDigest
		if intended {
			digest = file.IntendedDigest
		}
		dir, err := dirs.forTarget(file.TargetPath)
		if err != nil {
			return false, err
		}
		got, err := digestJournalLeaf(dir, filepath.Base(file.TargetPath))
		if errors.Is(err, os.ErrNotExist) {
			if !intended && !file.HadPrevious {
				continue
			}
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if got != digest {
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
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("restore quick_check iteration: %w", err)
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

func refreshRestoreDatabaseDigest(dirs restoreJournalDirs, journal *restoreTransactionJournal, databasePath string) error {
	if databasePath == "" {
		return nil
	}
	dir, err := dirs.forTarget(databasePath)
	if err != nil {
		return err
	}
	digest, err := digestJournalLeaf(dir, filepath.Base(databasePath))
	if err != nil {
		return err
	}
	for index := range journal.Files {
		if journal.Files[index].Name == "veil.db" {
			journal.Files[index].IntendedDigest = digest
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

func removeRestoreJournal(rootDir *safefs.Dir) error {
	if err := restoreJournalRemove(rootDir, restoreTransactionJournalName); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return rootDir.File().Sync()
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
