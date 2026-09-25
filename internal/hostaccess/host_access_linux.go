//go:build linux

package hostaccess

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// openManagedNoFollow opens path with O_NOFOLLOW so the returned descriptor
// is pinned to the inode that exists at open time. Migrate runs as root over
// trees owned by the service accounts, so a symlink swapped in between the
// walker's lstat and the chmod/chown must be rejected rather than followed
// to an attacker-chosen target (#1009).
func openManagedNoFollow(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("open managed path %s", path)
	}
	return file, nil
}
