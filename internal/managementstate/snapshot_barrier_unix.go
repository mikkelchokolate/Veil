//go:build unix

package managementstate

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// snapshotBarrierLock takes the exclusive barrier lock on file, blocking
// until it is held (flock LOCK_EX semantics, retried across EINTR).
func snapshotBarrierLock(file *os.File) error {
	for {
		err := unix.Flock(int(file.Fd()), unix.LOCK_EX)
		if !errors.Is(err, unix.EINTR) {
			return err
		}
	}
}

func snapshotBarrierUnlock(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}
