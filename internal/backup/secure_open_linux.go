//go:build linux

package backup

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// openBackupRegularNoFollow opens path for reading and verifies that the
// descriptor is a regular file. O_NOFOLLOW rejects a symlinked leaf, and
// O_NONBLOCK keeps a regular file swapped for a FIFO between the caller's
// check and this open from blocking the root helper on open(2) forever
// (#1254 F5, the #1083 class). A FIFO, directory, device or socket is refused
// after fstat on the held descriptor, so it is never handed to a reader as an
// empty file. O_NONBLOCK is cleared before returning so callers keep normal
// blocking read semantics.
func openBackupRegularNoFollow(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return nil, errors.Join(err, unix.Close(fd))
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, errors.Join(
			fmt.Errorf("refuse to open backup file %s: not a regular file", path),
			unix.Close(fd),
		)
	}
	if err := unix.SetNonblock(fd, false); err != nil {
		return nil, errors.Join(err, unix.Close(fd))
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("open backup file %s", path)
	}
	return file, nil
}
