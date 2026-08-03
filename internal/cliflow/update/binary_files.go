package update

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type atomicFile interface {
	io.Writer
	io.Closer
	Name() string
}

var (
	createTempForReplace = func(dir, pattern string) (atomicFile, error) { return os.CreateTemp(dir, pattern) }
	chmodForReplace      = os.Chmod
	removeForReplace     = os.Remove
	renameForReplace     = os.Rename
)

type BinaryFiles struct{}

func NewBinaryFiles() BinaryFiles {
	return BinaryFiles{}
}

func (BinaryFiles) Copy(src, dst string) (resultErr error) {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()
	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, dstFile.Close())
	}()
	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}
	return dstFile.Sync()
}

func (BinaryFiles) ReplaceAtomic(dst string, data []byte) error {
	dir := filepath.Dir(dst)
	tmp, err := createTempForReplace(dir, ".veil-update-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		removeForReplace(tmpPath)
		return err
	}
	if syncer, ok := tmp.(interface{ Sync() error }); ok {
		if err := syncer.Sync(); err != nil {
			tmp.Close()
			removeForReplace(tmpPath)
			return err
		}
	}
	if err := tmp.Close(); err != nil {
		removeForReplace(tmpPath)
		return err
	}
	if err := chmodForReplace(tmpPath, 0o755); err != nil {
		removeForReplace(tmpPath)
		return err
	}
	if err := renameForReplace(tmpPath, dst); err != nil {
		removeForReplace(tmpPath)
		return err
	}
	return syncUpdateDirectory(dir)
}

func (f BinaryFiles) Rollback(backupPath, currentPath string) error {
	backupData, err := os.ReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("read backup: %w", err)
	}
	if err := f.ReplaceAtomic(currentPath, backupData); err != nil {
		return fmt.Errorf("restore backup: %w", err)
	}
	// Best-effort cleanup of the backup.
	_ = os.Remove(backupPath)
	return nil
}

type binaryActivationJournal struct {
	Version    int    `json:"version"`
	TargetPath string `json:"targetPath"`
	BackupPath string `json:"backupPath"`
	OldDigest  string `json:"oldDigest"`
	NewDigest  string `json:"newDigest"`
	Phase      string `json:"phase"`
}

func syncUpdateDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func binaryDigest(body []byte) string {
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

func binaryFileDigest(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return binaryDigest(body), nil
}

func binaryActivationJournalPath(target string) string {
	return filepath.Join(filepath.Dir(target), ".veil-update-activation.json")
}

func writeBinaryActivationJournal(path string, journal binaryActivationJournal) error {
	body, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".veil-update-journal-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(body); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return syncUpdateDirectory(filepath.Dir(path))
}

func RecoverBinaryActivation(currentPath string) error {
	journalPath := binaryActivationJournalPath(currentPath)
	body, err := os.ReadFile(journalPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var journal binaryActivationJournal
	if err := json.Unmarshal(body, &journal); err != nil {
		return fmt.Errorf("decode binary activation journal: %w", err)
	}
	if journal.Version != 1 || filepath.Clean(journal.TargetPath) != filepath.Clean(currentPath) || len(journal.NewDigest) != 64 {
		return errors.New("invalid binary activation journal")
	}
	currentDigest, digestErr := binaryFileDigest(currentPath)
	if digestErr == nil && currentDigest == journal.NewDigest {
		if err := os.Remove(journalPath); err != nil {
			return err
		}
		return syncUpdateDirectory(filepath.Dir(currentPath))
	}
	if (digestErr == nil && journal.Phase == "intent" && currentDigest == journal.OldDigest) ||
		(errors.Is(digestErr, os.ErrNotExist) && journal.Phase == "intent" && journal.OldDigest == "") {
		if err := os.Remove(journalPath); err != nil {
			return err
		}
		return syncUpdateDirectory(filepath.Dir(currentPath))
	}
	backupDigest, backupErr := binaryFileDigest(journal.BackupPath)
	if backupErr != nil || backupDigest != journal.OldDigest {
		return errors.New("binary activation is ambiguous and exact backup is unavailable")
	}
	backupBody, err := os.ReadFile(journal.BackupPath)
	if err != nil {
		return err
	}
	if err := NewBinaryFiles().ReplaceAtomic(currentPath, backupBody); err != nil {
		return err
	}
	if restoredDigest, err := binaryFileDigest(currentPath); err != nil || restoredDigest != journal.OldDigest {
		return errors.New("post-rollback binary digest mismatch")
	}
	if err := os.Remove(journalPath); err != nil {
		return err
	}
	return syncUpdateDirectory(filepath.Dir(currentPath))
}

func ReplaceBinaryFromArchive(currentPath string, archive []byte, yes bool) (string, error) {
	if err := RecoverBinaryActivation(currentPath); err != nil {
		return "", err
	}
	binary, err := ExtractVeilBinary(archive)
	if err != nil {
		return "", fmt.Errorf("extract binary: %w", err)
	}
	backupPath := currentPath + ".backup"
	if !yes {
		return backupPath, fmt.Errorf("update requires --yes to confirm replacing %s", currentPath)
	}
	oldDigest, digestErr := binaryFileDigest(currentPath)
	if digestErr != nil && !errors.Is(digestErr, os.ErrNotExist) {
		return backupPath, fmt.Errorf("hash current binary: %w", digestErr)
	}
	if oldDigest != "" {
		if err := CopyFileData(currentPath, backupPath); err != nil {
			return backupPath, fmt.Errorf("backup: %w", err)
		}
	}
	journal := binaryActivationJournal{
		Version: 1, TargetPath: currentPath, BackupPath: backupPath,
		OldDigest: oldDigest, NewDigest: binaryDigest(binary), Phase: "intent",
	}
	journalPath := binaryActivationJournalPath(currentPath)
	if err := writeBinaryActivationJournal(journalPath, journal); err != nil {
		return backupPath, fmt.Errorf("write activation journal: %w", err)
	}
	if err := ReplaceBinaryAtomic(currentPath, binary); err != nil {
		return backupPath, fmt.Errorf("replace binary: %w", err)
	}
	if activeDigest, err := binaryFileDigest(currentPath); err != nil || activeDigest != journal.NewDigest {
		return backupPath, errors.New("post-activation binary digest mismatch")
	}
	journal.Phase = "active-verified"
	if err := writeBinaryActivationJournal(journalPath, journal); err != nil {
		return backupPath, err
	}
	if err := os.Remove(journalPath); err != nil {
		return backupPath, err
	}
	if err := syncUpdateDirectory(filepath.Dir(currentPath)); err != nil {
		return backupPath, err
	}
	return backupPath, nil
}

func CopyFileData(src, dst string) error {
	return NewBinaryFiles().Copy(src, dst)
}

// ReplaceBinaryAtomic writes data to a temp file in dst's directory and renames it over dst.
var ReplaceBinaryAtomic = func(dst string, data []byte) error {
	return NewBinaryFiles().ReplaceAtomic(dst, data)
}

// RollbackBinary copies the backup file back over the current binary and
// removes the backup. Returns an error if the rollback cannot be completed.
func RollbackBinary(backupPath, currentPath string) error {
	return NewBinaryFiles().Rollback(backupPath, currentPath)
}
