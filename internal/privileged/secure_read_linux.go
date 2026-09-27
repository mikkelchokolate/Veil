//go:build linux

package privileged

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// openRegularNoFollow opens path and verifies that the resulting descriptor
// is a regular file — not a symlink (O_NOFOLLOW rejects ELOOP on the leaf),
// not a FIFO/device/socket — while the fd pins the inode for the bounded read.
// O_NONBLOCK is set so a regular file swapped for a FIFO between the caller's
// check and this open cannot block the privileged helper (#1083); the flag is
// cleared before the caller reads so the fd has normal blocking semantics.
func openRegularNoFollow(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("open managed file %s", path)
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, errors.Join(
			fmt.Errorf("refuse to read %s: not a regular file", path),
			file.Close(),
		)
	}
	// Drop O_NONBLOCK: the flag is harmless on regular files but a stale
	// non-blocking descriptor could surface EAGAIN on exotic filesystems.
	if err := unix.SetNonblock(fd, false); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}
