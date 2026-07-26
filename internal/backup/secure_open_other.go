//go:build !linux

package backup

import (
	"errors"
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
	return os.Open(path)
}
