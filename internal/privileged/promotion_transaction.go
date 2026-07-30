package privileged

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"

	"github.com/google/uuid"
	"github.com/mikkelchokolate/Veil/internal/atomicfile"
)

const promotionTransactionJournalName = ".promotion-transaction.json"

type promotionTransactionJournal struct {
	Version       int               `json:"version"`
	TransactionID string            `json:"transactionId"`
	Kind          string            `json:"kind"`
	Phase         string            `json:"phase"`
	ManifestPath  string            `json:"manifestPath"`
	Manifest      promotionManifest `json:"manifest"`
}

type preparedPromotionOperation struct {
	artifact      ResolvedArtifact
	remove        bool
	body          []byte
	symlinkTarget string
}

func executePromotionTransaction(backupRoot string, now func() time.Time, kind string, writes, removes []ResolvedArtifact) (result PromoteResult, resultErr error) {
	if len(writes) == 0 && len(removes) == 0 && backupRoot == "" {
		return PromoteResult{}, nil
	}
	if backupRoot == "" {
		return PromoteResult{}, errors.New("promotion backup root is required")
	}
	if err := os.MkdirAll(backupRoot, 0o700); err != nil {
		return PromoteResult{}, fmt.Errorf("create promotion backup root: %w", err)
	}
	lockFile, err := os.OpenFile(filepath.Join(backupRoot, ".promotion.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return PromoteResult{}, fmt.Errorf("open promotion lock: %w", err)
	}
	defer func() {
		resultErr = errors.Join(resultErr, releaseLockedFile(lockFile))
	}()
	if err := unix.Flock(int(lockFile.Fd()), unix.LOCK_EX); err != nil {
		return PromoteResult{}, fmt.Errorf("lock promotion root: %w", err)
	}
	if err := recoverPromotionTransaction(backupRoot); err != nil {
		return PromoteResult{}, fmt.Errorf("recover interrupted promotion: %w", err)
	}
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
		ManifestPath: manifestPath,
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
			if err := atomicfile.Write(safetyPath, []byte(linkTarget), 0o600, 0o700); err != nil {
				return err
			}
			record.BackupPath, record.SafetyPath = safetyPath, safetyPath
			record.HadPrevious, record.WasSymlink = true, true
			record.OldLinkTarget = linkTarget
			record.OldDigest = promotionDigest([]byte(linkTarget))
		} else {
			var err error
			record, err = backupPromotionDestination(backupRoot, backupID, artifact)
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
			oldBody, err := readManagedConfigFile(record.BackupPath)
			if err != nil {
				return err
			}
			record.OldDigest = promotionDigest(oldBody)
		}
		operation := preparedPromotionOperation{artifact: artifact, remove: remove}
		var err error
		if artifact.SymlinkTarget != "" {
			operation.symlinkTarget = artifact.SymlinkTarget
			record.Operation = "symlink"
			record.NewDigest = promotionDigest([]byte(artifact.SymlinkTarget))
		} else if !remove {
			operation.body, err = readManagedConfigFile(artifact.Source)
			if err != nil {
				return err
			}
			record.NewDigest = promotionDigest(operation.body)
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
	if err := writePromotionJournal(backupRoot, journal); err != nil {
		return PromoteResult{}, err
	}

	result = PromoteResult{BackupID: backupID}
	for index, operation := range operations {
		var err error
		if operation.remove {
			err = removePromotionDestination(operation.artifact.Destination)
			if err == nil {
				result.RemovedArtifacts = append(result.RemovedArtifacts, operation.artifact.ID)
			}
		} else if operation.symlinkTarget != "" {
			err = writePromotionSymlink(operation.artifact.Destination, operation.symlinkTarget)
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
			return PromoteResult{}, rollbackPromotionAfterError(backupRoot, journal, err)
		}
		journal.Manifest.Records[index].Phase = "published"
		journal.Phase = fmt.Sprintf("artifact-%d-published", index+1)
		if err := writePromotionJournal(backupRoot, journal); err != nil {
			return PromoteResult{}, rollbackPromotionAfterError(backupRoot, journal, err)
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
		return PromoteResult{}, rollbackPromotionAfterError(backupRoot, journal, err)
	}
	if err := atomicfile.Write(manifestPath, manifestBody, 0o600, 0o700); err != nil {
		return PromoteResult{}, rollbackPromotionAfterError(backupRoot, journal, err)
	}
	journal.Phase = "manifest-committed"
	if err := writePromotionJournal(backupRoot, journal); err != nil {
		return PromoteResult{}, rollbackPromotionAfterError(backupRoot, journal, err)
	}
	if err := removePromotionJournal(backupRoot); err != nil {
		return PromoteResult{}, err
	}
	return result, nil
}

func rollbackPromotionAfterError(root string, journal promotionTransactionJournal, cause error) error {
	if err := restorePromotionPreTransaction(root, &journal); err != nil {
		return fmt.Errorf("%v; automatic promotion rollback failed: %w", cause, err)
	}
	return cause
}

func recoverPromotionTransaction(root string) error {
	path := filepath.Join(root, promotionTransactionJournalName)
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var journal promotionTransactionJournal
	if err := json.Unmarshal(body, &journal); err != nil {
		return fmt.Errorf("decode promotion transaction journal: %w", err)
	}
	if journal.Version != 1 || journal.TransactionID == "" {
		return errors.New("invalid promotion transaction journal")
	}
	return restorePromotionPreTransaction(root, &journal)
}

func restorePromotionPreTransaction(root string, journal *promotionTransactionJournal) error {
	journal.Phase = "rolling-back"
	_ = writePromotionJournal(root, *journal)
	for index := len(journal.Manifest.Records) - 1; index >= 0; index-- {
		record := &journal.Manifest.Records[index]
		if record.HadPrevious {
			if record.SafetyPath == "" || !pathWithin(root, record.SafetyPath) {
				return fmt.Errorf("promotion safety path is outside backup root for %s", record.ArtifactID)
			}
			if record.WasSymlink {
				if record.OldDigest != "" && promotionDigest([]byte(record.OldLinkTarget)) != record.OldDigest {
					return fmt.Errorf("promotion symlink metadata digest mismatch for %s", record.ArtifactID)
				}
				if err := writePromotionSymlink(record.Destination, record.OldLinkTarget); err != nil {
					return err
				}
			} else {
				body, err := readManagedConfigFile(record.SafetyPath)
				if err != nil {
					return err
				}
				if record.OldDigest != "" && promotionDigest(body) != record.OldDigest {
					return fmt.Errorf("promotion safety digest mismatch for %s", record.ArtifactID)
				}
				if err := atomicfile.Write(record.Destination, body, 0o600, 0o700); err != nil {
					return err
				}
			}
		} else if err := removePromotionDestination(record.Destination); err != nil {
			return err
		}
		record.Phase = "rolled-back"
		journal.Phase = fmt.Sprintf("artifact-%d-rolled-back", index+1)
		if err := writePromotionJournal(root, *journal); err != nil {
			return err
		}
	}
	if journal.ManifestPath != "" {
		if err := os.Remove(journal.ManifestPath); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		} else if err := syncPromotionParent(journal.ManifestPath); err != nil {
			return err
		}
	}
	journal.Phase = "rolled-back"
	if err := writePromotionJournal(root, *journal); err != nil {
		return err
	}
	return removePromotionJournal(root)
}

func writePromotionJournal(root string, journal promotionTransactionJournal) error {
	body, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(root, promotionTransactionJournalName), body, 0o600, 0o700)
}

func removePromotionJournal(root string) error {
	path := filepath.Join(root, promotionTransactionJournalName)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncPromotionParent(path)
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

func writePromotionSymlink(path, target string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Symlink(target, path); err != nil {
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
