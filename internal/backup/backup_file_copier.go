package backup

import (
	"fmt"
	"io"
	"os"
)

type FileCopier struct{}

type BackupFileCopier = FileCopier

func NewFileCopier() FileCopier { return FileCopier{} }

func NewBackupFileCopier() BackupFileCopier { return NewFileCopier() }

// fileCopierSync is overridable in tests to avoid expensive fsync under the race detector.
var fileCopierSync = (*os.File).Sync

// Copy copies a file from src to dst preserving the given mode and syncing the destination.
func (FileCopier) Copy(src, dst string, mode os.FileMode) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("copy: %w", err)
	}
	return fileCopierSync(dstFile)
}
