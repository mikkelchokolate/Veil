//go:build unix

package managementstate

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// verifySnapshotBarrierLinks rejects a barrier inode carrying extra hard
// links: a second name into a shared inode would let a swap merge this lock
// with a different flock target and silently alias barrier domains (#1216).
func verifySnapshotBarrierLinks(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("management snapshot barrier metadata is unavailable")
	}
	if stat.Nlink != 1 {
		return errors.New("management snapshot barrier has an unsafe link count")
	}
	return nil
}

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
