//go:build linux

package backup

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func openBackupRegularNoFollow(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("open backup file %s", path)
	}
	return file, nil
}
