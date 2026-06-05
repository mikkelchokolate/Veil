//go:build !linux

package privileged

import (
	"errors"
	"os"
)

func openRegularNoFollow(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("managed file must not be a symlink")
	}
	return os.Open(path)
}
