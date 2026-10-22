package privileged

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/mikkelchokolate/Veil/internal/atomicfile"
	"github.com/mikkelchokolate/Veil/internal/safefs"
)

const promotionTransactionJournalName = ".promotion-transaction.json"

// promotionJournalRemove unlinks the journal leaf inside the pinned backup
// root; it is a hook so tests can inject a committed-marker unlink failure.
var promotionJournalRemove = func(rootDir *safefs.Dir, name string) error { return rootDir.RemoveAt(name) }

type promotionTransactionJournal struct {
	Version         int               `json:"version"`
	TransactionID   string            `json:"transactionId"`
	Kind            string            `json:"kind"`
	Phase           string            `json:"phase"`
	ManifestPath    string            `json:"manifestPath"`
	Manifest        promotionManifest `json:"manifest"`
	FenceGeneration uint64            `json:"fenceGeneration"`
}

type preparedPromotionOperation struct {
	artifact      ResolvedArtifact
	remove        bool
	body          []byte
	symlinkTarget string
}

// executePromotionTransaction is the un-fenced variant kept for tests and
// one-off callers: it passes a nil destination policy, which fails closed on
// recovery (validatePromotionDestinations) rather than replaying an
// unauthenticated journal — production apply paths must use
// executePromotionTransactionFenced with a real policy.
func executePromotionTransaction(backupRoot string, now func() time.Time, kind string, writes, removes []ResolvedArtifact) (result PromoteResult, resultErr error) {
	return executePromotionTransactionFenced(backupRoot, now, kind, writes, removes, 0, nil)
}

func executePromotionTransactionFenced(backupRoot string, now func() time.Time, kind string, writes, removes []ResolvedArtifact,
	fenceGeneration uint64, validateDestination func(string, string) bool) (result PromoteResult, resultErr error) {
	if len(writes) == 0 && len(removes) == 0 && backupRoot == "" {
		return PromoteResult{}, nil
	}
	resultErr = withPromotionLock(backupRoot, validateDestination, fenceGeneration, func(backupDir *safefs.Dir) error {
		var err error
		result, err = executePromotionTransactionLocked(backupDir, backupRoot, now, kind, writes, removes, fenceGeneration)
		return err
	})
	if resultErr != nil {
		return PromoteResult{}, resultErr
	}
	return result, nil
}

// withPromotionLock pins the backup root, takes the exclusive .promotion.lock
// flock, recovers any interrupted transaction under that lock, and only then
// runs fn (nil fn = recovery only). Recovery must never run outside the lock:
// it replays or rolls back whatever journal is on disk, and an unlocked
// recovery racing a concurrent promotion that holds the lock mid-publish
// would roll back that live transaction's published artifacts and delete its
// journal, leaving a torn mix at the managed destinations (#1257 F2). flock
// is per open file description, so fn must not call back into anything that
// takes this lock again.
func withPromotionLock(backupRoot string, validateDestination func(string, string) bool, fenceGeneration uint64,
	fn func(backupDir *safefs.Dir) error) (resultErr error) {
	if backupRoot == "" {
		return errors.New("promotion backup root is required")
	}
	// Pin the backup root on a descriptor and refuse a symlinked lock leaf:
	// the root lives under the service-writable state tree, so a planted
	// symlink would otherwise redirect a root-owned O_CREATE open (#1229-F5).
	// The destination policy (validatePromotionDestinations) authenticates
	// only ArtifactID/Destination; the SafetyPath/ManifestPath members a
	// recovered journal reads and deletes are trusted only because the root
	// is the helper-owned 0700 directory, the same contract the firewall
	// transaction root enforces on this very directory (#1227, #1274).
	backupDir, err := openPromotionBackupRoot(backupRoot)
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, backupDir.Close())
	}()
	lockFile, err := openLockFileAt(backupDir, ".promotion.lock")
	if err != nil {
		return fmt.Errorf("open promotion lock: %w", err)
	}
	defer func() {
		resultErr = errors.Join(resultErr, releaseLockedFile(lockFile))
	}()
	if err := lockFileExclusive(lockFile); err != nil {
		return fmt.Errorf("lock promotion root: %w", err)
	}
	if err := recoverPromotionTransactionAt(backupDir, backupRoot, validateDestination, fenceGeneration); err != nil {
		return fmt.Errorf("recover interrupted promotion: %w", err)
	}
	if fn == nil {
		return nil
	}
	return fn(backupDir)
}

// executePromotionTransactionLocked runs one promotion transaction. The
// caller must hold the promotion lock on backupDir (withPromotionLock) and
// have recovered any interrupted journal under it.
func executePromotionTransactionLocked(backupDir *safefs.Dir, backupRoot string, now func() time.Time, kind string,
	writes, removes []ResolvedArtifact, fenceGeneration uint64) (PromoteResult, error) {
	if len(writes) == 0 && len(removes) == 0 {
		return PromoteResult{}, nil
	}

	if now == nil {
		now = time.Now
	}
	backupID := now().UTC().Format("20060102T150405.000000000Z")
	if kind == "rollback" {
		backupID = "rollback-" + backupID + "-" + uuid.NewString()
	}
	transactionID := uuid.NewString()
	manifestPath := filepath.Join(backupRoot, backupID, "manifest.json")
	journal := promotionTransactionJournal{
		Version: 1, TransactionID: transactionID, Kind: kind, Phase: "prepared",
		ManifestPath: manifestPath, FenceGeneration: fenceGeneration,
		Manifest: promotionManifest{
			Version: 1, TransactionID: transactionID, BackupID: backupID,
			Kind: kind, Phase: "prepared",
		},
	}
	operations := make([]preparedPromotionOperation, 0, len(writes)+len(removes))
	seenDestinations := map[string]struct{}{}
	prepare := func(artifact ResolvedArtifact, remove bool) error {
		if artifact.ID == "" || artifact.Destination == "" {
			return errors.New("promotion artifact id and destination are required")
		}
		cleanDestination := filepath.Clean(artifact.Destination)
		if _, exists := seenDestinations[cleanDestination]; exists {
			return fmt.Errorf("duplicate promotion destination %s", cleanDestination)
		}
		seenDestinations[cleanDestination] = struct{}{}
		// A staged source lives in the veil-writable staging root: read and
		// authenticate it against the caller's digest before touching the
		// destination or the backup root, so rewritten staged bytes are
		// refused outright instead of after a safety copy was taken (#1269).
		var stagedBody []byte
		if !remove && artifact.SymlinkTarget == "" && artifact.sourceBody == nil {
			if artifact.expectedDigest == "" {
				return fmt.Errorf("staged promotion artifact %s has no expected digest", artifact.ID)
			}
			body, err := readManagedConfigFile(artifact.Source)
			if err != nil {
				return err
			}
			if promotionDigest(body) != artifact.expectedDigest {
				return newError(ErrorConflict, fmt.Sprintf("staged promotion artifact %s does not match its expected digest", artifact.ID))
			}
			stagedBody = body
		}
		info, statErr := os.Lstat(cleanDestination)
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
		isDestinationSymlink := statErr == nil && info.Mode()&os.ModeSymlink != 0
		if isDestinationSymlink && !remove {
			return fmt.Errorf("promotion destination is a symlink: %s", cleanDestination)
		}
		record := promotionManifestRecord{ArtifactID: artifact.ID, Destination: artifact.Destination}
		if isDestinationSymlink {
			linkTarget, err := os.Readlink(cleanDestination)
			if err != nil {
				return err
			}
			cleanID := filepath.Clean(artifact.ID)
			prefix := ".." + string(filepath.Separator)
			if cleanID == "." || filepath.IsAbs(cleanID) || cleanID == ".." || len(cleanID) >= len(prefix) && cleanID[:len(prefix)] == prefix {
				return fmt.Errorf("invalid promotion artifact id %q", artifact.ID)
			}
			safetyPath := filepath.Join(backupRoot, backupID, cleanID)
			if err := writePromotionBackupMember(backupDir, backupRoot, safetyPath, []byte(linkTarget)); err != nil {
				return err
			}
			record.BackupPath, record.SafetyPath = safetyPath, safetyPath
			record.HadPrevious, record.WasSymlink = true, true
			record.OldLinkTarget = linkTarget
			record.OldDigest = promotionDigest([]byte(linkTarget))
		} else {
			var err error
			record, err = backupPromotionDestination(backupDir, backupRoot, backupID, artifact)
			if err != nil {
				return err
			}
		}
		record.TransactionID = transactionID
		record.SafetyPath = record.BackupPath
		record.Operation = "write"
		if remove {
			record.Operation = "remove"
		}
		record.Phase = "prepared"
		if record.HadPrevious && !record.WasSymlink {
			oldBody, err := readPromotionBackupMember(backupDir, backupRoot, record.BackupPath)
			if err != nil {
				return err
			}
			record.OldDigest = promotionDigest(oldBody)
		}
		operation := preparedPromotionOperation{artifact: artifact, remove: remove}
		if artifact.SymlinkTarget != "" {
			operation.symlinkTarget = artifact.SymlinkTarget
			record.Operation = "symlink"
			record.NewDigest = promotionDigest([]byte(artifact.SymlinkTarget))
		} else if !remove && artifact.sourceBody != nil {
			// A restore source was already read descriptor-relative from the
			// pinned backup root; re-resolving its path here would reopen the
			// swap window the pinned read closed (#1274).
			operation.body = artifact.sourceBody
			record.NewDigest = promotionDigest(operation.body)
		} else if !remove {
			operation.body = stagedBody
			record.NewDigest = artifact.expectedDigest
		}
		operations = append(operations, operation)
		journal.Manifest.Records = append(journal.Manifest.Records, record)
		return nil
	}
	// Read and durably stage every source/safety artifact before publishing the
	// first destination. A preflight error therefore cannot create a mixed set.
	for _, artifact := range writes {
		if err := prepare(artifact, false); err != nil {
			return PromoteResult{}, err
		}
	}
	for _, artifact := range removes {
		if err := prepare(artifact, true); err != nil {
			return PromoteResult{}, err
		}
	}
	if err := writePromotionJournal(backupDir, journal); err != nil {
		return PromoteResult{}, err
	}

	result := PromoteResult{BackupID: backupID}
	for index, operation := range operations {
		var err error
		if operation.remove {
			err = removePromotionDestination(operation.artifact.Destination)
			if err == nil {
				result.RemovedArtifacts = append(result.RemovedArtifacts, operation.artifact.ID)
			}
		} else if operation.symlinkTarget != "" {
			err = writePromotionSymlink(operation.artifact.ID, operation.artifact.Destination, operation.symlinkTarget)
			if err == nil {
				result.WrittenArtifacts = append(result.WrittenArtifacts, operation.artifact.ID)
			}
		} else {
			err = atomicfile.Write(operation.artifact.Destination, operation.body, 0o600, 0o700)
			if err == nil {
				err = ensureRuntimeArtifactOwnership(operation.artifact.ID, operation.artifact.Destination)
			}
			if err == nil {
				result.WrittenArtifacts = append(result.WrittenArtifacts, operation.artifact.ID)
			}
		}
		if err != nil {
			return PromoteResult{}, rollbackPromotionAfterError(backupDir, backupRoot, journal, err)
		}
		journal.Manifest.Records[index].Phase = "published"
		journal.Phase = fmt.Sprintf("artifact-%d-published", index+1)
		if err := writePromotionJournal(backupDir, journal); err != nil {
			return PromoteResult{}, rollbackPromotionAfterError(backupDir, backupRoot, journal, err)
		}
		if safety := journal.Manifest.Records[index].SafetyPath; safety != "" {
			result.BackupArtifacts = append(result.BackupArtifacts, safety)
		}
	}
	for index := range journal.Manifest.Records {
		journal.Manifest.Records[index].Phase = "committed"
	}
	journal.Manifest.Phase = "committed"
	manifestBody, err := json.Marshal(journal.Manifest)
	if err != nil {
		return PromoteResult{}, rollbackPromotionAfterError(backupDir, backupRoot, journal, err)
	}
	if err := writePromotionBackupMember(backupDir, backupRoot, manifestPath, manifestBody); err != nil {
		return PromoteResult{}, rollbackPromotionAfterError(backupDir, backupRoot, journal, err)
	}
	journal.Phase = "manifest-committed"
	if err := writePromotionJournal(backupDir, journal); err != nil {
		return PromoteResult{}, rollbackPromotionAfterError(backupDir, backupRoot, journal, err)
	}
	if err := removePromotionJournal(backupDir); err != nil {
		return PromoteResult{}, err
	}
	return result, nil
}

func rollbackPromotionAfterError(rootDir *safefs.Dir, root string, journal promotionTransactionJournal, cause error) error {
	if err := restorePromotionPreTransaction(rootDir, root, &journal); err != nil {
		return fmt.Errorf("%v; automatic promotion rollback failed: %w", cause, err)
	}
	return cause
}

func recoverPromotionTransactionWithPolicy(root string, validateDestination func(string, string) bool, acceptedGeneration uint64) error {
	if root == "" {
		// No configured root means no journal can exist to recover; every
		// mutating path rejects the empty root on its own.
		return nil
	}
	// Recovery takes the promotion lock like every transaction does, so it
	// can never roll back a concurrent transaction mid-publish (#1257 F2).
	return withPromotionLock(root, validateDestination, acceptedGeneration, nil)
}

// recoverPromotionTransactionAt replays or rolls back an interrupted
// transaction found under the pinned, helper-owned backup root. The journal
// and every backup-tree member it names (SafetyPath, ManifestPath) are read
// and removed descriptor-relative to rootDir, so neither a swapped root nor a
// symlinked intermediate directory can redirect them (#1274); every
// destination is re-validated against the caller's live policy (#1218, #1228).
func recoverPromotionTransactionAt(rootDir *safefs.Dir, root string, validateDestination func(string, string) bool, acceptedGeneration uint64) error {
	body, err := readPromotionJournalAt(rootDir)
	if err != nil {
		return err
	}
	if body == nil {
		return nil
	}
	var journal promotionTransactionJournal
	if err := json.Unmarshal(body, &journal); err != nil {
		return fmt.Errorf("decode promotion transaction journal: %w", err)
	}
	if journal.Version != 1 || journal.TransactionID == "" {
		return errors.New("invalid promotion transaction journal")
	}
	if journal.FenceGeneration > 0 && acceptedGeneration > 0 && acceptedGeneration < journal.FenceGeneration {
		return errors.New("promotion recovery fencing generation regressed")
	}
	if err := validatePromotionDestinations(journal, validateDestination); err != nil {
		return err
	}
	if journal.Phase == "manifest-committed" && journal.Manifest.Phase == "committed" {
		if err := verifyCommittedPromotion(rootDir, root, journal); err != nil {
			return err
		}
		return removePromotionJournal(rootDir)
	}
	return restorePromotionPreTransaction(rootDir, root, &journal)
}

func validatePromotionDestinations(journal promotionTransactionJournal, validate func(string, string) bool) error {
	// The journal is persisted under the service-writable backup root, so it
	// is attacker-authored: without the caller's destination policy there is
	// no proof its records still name managed paths, and recovery must fail
	// closed instead of trusting them (#1218, #1228).
	if validate == nil {
		return errors.New("promotion journal recovery requires a destination policy")
	}
	for _, record := range journal.Manifest.Records {
		if !validate(record.ArtifactID, record.Destination) {
			return fmt.Errorf("promotion destination is no longer permitted for %s", record.ArtifactID)
		}
	}
	return nil
}

func verifyCommittedPromotion(rootDir *safefs.Dir, root string, journal promotionTransactionJournal) error {
	if journal.ManifestPath == "" || !pathWithin(root, journal.ManifestPath) {
		return errors.New("committed promotion manifest escapes backup root")
	}
	body, err := readPromotionBackupMember(rootDir, root, journal.ManifestPath)
	if err != nil {
		return err
	}
	expected, err := json.Marshal(journal.Manifest)
	if err != nil {
		return err
	}
	if !bytes.Equal(body, expected) {
		return errors.New("committed promotion manifest does not match journal")
	}
	for _, record := range journal.Manifest.Records {
		if record.Phase != "committed" {
			return fmt.Errorf("promotion artifact %s is not committed", record.ArtifactID)
		}
		switch record.Operation {
		case "remove":
			if _, err := os.Lstat(record.Destination); !errors.Is(err, os.ErrNotExist) {
				if err == nil {
					return fmt.Errorf("removed promotion artifact still exists: %s", record.ArtifactID)
				}
				return err
			}
		case "symlink":
			target, err := os.Readlink(record.Destination)
			if err != nil || promotionDigest([]byte(target)) != record.NewDigest {
				return fmt.Errorf("committed promotion symlink mismatch for %s", record.ArtifactID)
			}
		default:
			body, err := readManagedConfigFile(record.Destination)
			if err != nil {
				return err
			}
			if promotionDigest(body) != record.NewDigest {
				return fmt.Errorf("committed promotion digest mismatch for %s", record.ArtifactID)
			}
		}
	}
	return nil
}

func restorePromotionPreTransaction(rootDir *safefs.Dir, root string, journal *promotionTransactionJournal) error {
	journal.Phase = "rolling-back"
	_ = writePromotionJournal(rootDir, *journal)
	for index := len(journal.Manifest.Records) - 1; index >= 0; index-- {
		record := &journal.Manifest.Records[index]
		if record.HadPrevious {
			if record.SafetyPath == "" || !pathWithin(root, record.SafetyPath) {
				return fmt.Errorf("promotion safety path is outside backup root for %s", record.ArtifactID)
			}
			// Every record the helper writes with HadPrevious carries the
			// digest of the bytes it backed up; an empty digest is not
			// "skip the check" but an unauthenticated record (#1274).
			if record.OldDigest == "" {
				return fmt.Errorf("promotion safety record for %s has no digest", record.ArtifactID)
			}
			if record.WasSymlink {
				if promotionDigest([]byte(record.OldLinkTarget)) != record.OldDigest {
					return fmt.Errorf("promotion symlink metadata digest mismatch for %s", record.ArtifactID)
				}
				if err := writePromotionSymlink(record.ArtifactID, record.Destination, record.OldLinkTarget); err != nil {
					return err
				}
			} else {
				body, err := readPromotionBackupMember(rootDir, root, record.SafetyPath)
				if err != nil {
					return err
				}
				if promotionDigest(body) != record.OldDigest {
					return fmt.Errorf("promotion safety digest mismatch for %s", record.ArtifactID)
				}
				if err := atomicfile.Write(record.Destination, body, 0o600, 0o700); err != nil {
					return err
				}
				if err := ensureRuntimeArtifactOwnership(record.ArtifactID, record.Destination); err != nil {
					return err
				}
			}
		} else if err := removePromotionDestination(record.Destination); err != nil {
			return err
		}
		record.Phase = "rolled-back"
		journal.Phase = fmt.Sprintf("artifact-%d-rolled-back", index+1)
		if err := writePromotionJournal(rootDir, *journal); err != nil {
			return err
		}
	}
	if journal.ManifestPath != "" {
		if !pathWithin(root, journal.ManifestPath) {
			return errors.New("promotion journal manifest path escapes backup root")
		}
		if err := removePromotionBackupMember(rootDir, root, journal.ManifestPath); err != nil {
			return err
		}
	}
	journal.Phase = "rolled-back"
	if err := writePromotionJournal(rootDir, *journal); err != nil {
		return err
	}
	return removePromotionJournal(rootDir)
}

// writePromotionJournal publishes the journal descriptor-relative inside the
// pinned backup root (temp sibling + renameat), so a root swapped after it was
// pinned cannot redirect the helper's own journal write (#1274).
func writePromotionJournal(rootDir *safefs.Dir, journal promotionTransactionJournal) error {
	body, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	return atomicWriteFileAt(rootDir, promotionTransactionJournalName, body, 0o600)
}

func removePromotionJournal(rootDir *safefs.Dir) error {
	if err := promotionJournalRemove(rootDir, promotionTransactionJournalName); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return rootDir.File().Sync()
}

func removePromotionDestination(path string) error {
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return syncPromotionParent(path)
}

func writePromotionSymlink(artifactID, path, target string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Symlink(target, path); err != nil {
		return err
	}
	// MkdirAll leaves fresh parents root:root 0700; restore the veil-proxy
	// traversal contract for the generated tree (audit #524).
	if err := ensureRuntimeArtifactSymlinkParents(artifactID, path); err != nil {
		return err
	}
	return syncPromotionParent(path)
}

func syncPromotionParent(path string) error {
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func promotionDigest(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
