//go:build !linux

package backup

import (
	"errors"
	"fmt"
	"os"
)

func openBackupRegularNoFollow(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("backup file must not be a symlink")
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("refuse to open backup file %s: not a regular file", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	// Re-check on the held descriptor: the leaf may have been swapped
	// between the Lstat above and the open.
	opened, err := file.Stat()
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}
	if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, errors.Join(fmt.Errorf("refuse to open backup file %s: changed during open or not a regular file", path), file.Close())
	}
	return file, nil
}
