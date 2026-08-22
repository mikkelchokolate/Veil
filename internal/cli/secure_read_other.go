//go:build !linux

package cli

import (
	"errors"
	"os"
)

func openCLIRegularNoFollow(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("CLI file must not be a symlink")
	}
	return os.Open(path)
}
