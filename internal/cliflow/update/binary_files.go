package update

import (
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

func (BinaryFiles) Copy(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()
	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer dstFile.Close()
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
	if err := tmp.Close(); err != nil {
		removeForReplace(tmpPath)
		return err
	}
	if err := chmodForReplace(tmpPath, 0o755); err != nil {
		removeForReplace(tmpPath)
		return err
	}
	return renameForReplace(tmpPath, dst)
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

func ReplaceBinaryFromArchive(currentPath string, archive []byte, yes bool) (string, error) {
	binary, err := ExtractVeilBinary(archive)
	if err != nil {
		return "", fmt.Errorf("extract binary: %w", err)
	}
	backupPath := currentPath + ".backup"
	if err := CopyFileData(currentPath, backupPath); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("backup: %w", err)
	}
	if !yes {
		return backupPath, fmt.Errorf("update requires --yes to confirm replacing %s", currentPath)
	}
	if err := ReplaceBinaryAtomic(currentPath, binary); err != nil {
		return backupPath, fmt.Errorf("replace binary: %w", err)
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
