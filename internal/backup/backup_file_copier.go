package backup

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mikkelchokolate/Veil/internal/safefs"
)

type FileCopier struct{}

type BackupFileCopier = FileCopier

func NewFileCopier() FileCopier { return FileCopier{} }

func NewBackupFileCopier() BackupFileCopier { return NewFileCopier() }

// fileCopierSync is overridable in tests to avoid expensive fsync under the race detector.
var fileCopierSync = (*os.File).Sync

// fileCopierCopy is overridable in tests to inject copy failures.
var fileCopierCopy = io.Copy

// Copy copies src to dst, setting mode (and prior ownership where a regular
// destination exists) on the open temp descriptor before a
// descriptor-relative rename — no metadata operation ever re-resolves a path
// under the destination directory (#1219).
func (FileCopier) Copy(src, dst string, mode os.FileMode) error {
	srcFile, err := safefs.OpenNoFollow(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer srcFile.Close()
	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	if !srcInfo.Mode().IsRegular() {
		return fmt.Errorf("backup source %s is not a regular file", src)
	}

	dirPath := filepath.Dir(dst)
	dir, err := safefs.OpenDir(dirPath)
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	defer dir.Close()
	tmp, tmpName, err := dir.CreateTempAt(".veil-copy-", 0o600)
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = dir.RemoveAt(tmpName)
		}
	}()

	if _, err := fileCopierCopy(tmp, srcFile); err != nil {
		return fmt.Errorf("copy: %w", err)
	}
	if err := tmp.Chmod(mode.Perm()); err != nil {
		return err
	}
	// Ownership preservation consults the destination leaf resolved relative
	// to the pinned parent — a swapped symlink reports as a symlink and is
	// refused rather than chown'ed (#1219).
	if existing, err := dir.StatAt(filepath.Base(dst)); err == nil {
		if !existing.Mode().IsRegular() || existing.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("restore destination is not a regular file: %s", dst)
		}
		uid, gid := fileOwnerIDs(existing)
		if err := restoreChownToMatch(tmp, uid, gid); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := fileCopierSync(tmp); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := dir.RenameAt(tmpName, filepath.Base(dst)); err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	committed = true
	return dir.File().Sync()
}
