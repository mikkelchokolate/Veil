//go:build !linux

package hostaccess

import (
	"errors"
	"os"
)

// openManagedNoFollow is the non-Linux fallback: O_NOFOLLOW is unavailable,
// so it refuses symlinks via lstat before opening. Migrate only runs on
// Linux installs; this keeps the package compiling elsewhere.
func openManagedNoFollow(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("managed path must not be a symlink")
	}
	return os.Open(path)
}
