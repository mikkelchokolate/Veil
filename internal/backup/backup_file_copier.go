package backup

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type FileCopier struct{}

type BackupFileCopier = FileCopier

func NewFileCopier() FileCopier { return FileCopier{} }

func NewBackupFileCopier() BackupFileCopier { return NewFileCopier() }

// fileCopierSync is overridable in tests to avoid expensive fsync under the race detector.
var fileCopierSync = (*os.File).Sync

// fileCopierCopy is overridable in tests to inject copy failures.
var fileCopierCopy = io.Copy

// Copy copies src to dst, setting mode on a sibling temp file before replace.
func (FileCopier) Copy(src, dst string, mode os.FileMode) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer srcFile.Close()

	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, ".veil-copy-*")
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := fileCopierCopy(tmp, srcFile); err != nil {
		return fmt.Errorf("copy: %w", err)
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		return err
	}
	if err := fileCopierSync(tmp); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if existing, err := os.Lstat(dst); err == nil {
		if err := restoreChownToMatch(tmpPath, existing); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	committed = true
	return syncDirectory(dir)
}
