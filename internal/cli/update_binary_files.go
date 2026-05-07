package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type UpdateBinaryFiles struct{}

func NewUpdateBinaryFiles() UpdateBinaryFiles {
	return UpdateBinaryFiles{}
}

func (UpdateBinaryFiles) Copy(src, dst string) error {
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

func (UpdateBinaryFiles) ReplaceAtomic(dst string, data []byte) error {
	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, ".veil-update-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, dst)
}

func (f UpdateBinaryFiles) Rollback(backupPath, currentPath string) error {
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

func copyFileData(src, dst string) error {
	return NewUpdateBinaryFiles().Copy(src, dst)
}

func replaceBinaryAtomic(dst string, data []byte) error {
	return NewUpdateBinaryFiles().ReplaceAtomic(dst, data)
}

// rollbackBinary copies the backup file back over the current binary and
// removes the backup. Returns an error if the rollback cannot be completed.
func rollbackBinary(backupPath, currentPath string) error {
	return NewUpdateBinaryFiles().Rollback(backupPath, currentPath)
}
