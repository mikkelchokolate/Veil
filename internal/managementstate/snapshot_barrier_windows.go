//go:build windows

package managementstate

import (
	"os"

	"golang.org/x/sys/windows"
)

// Windows has no flock(2); LockFileEx on the same byte range of the lock file
// gives the same exclusive, blocking, per-handle mutual exclusion (the same
// approach #1077 used for the runtime activation lock, issues #1011/#1085).
// A lock region past EOF is legal, so the empty lock file works unchanged.
func snapshotBarrierLock(file *os.File) error {
	overlapped := &windows.Overlapped{}
	return windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, overlapped)
}

func snapshotBarrierUnlock(file *os.File) error {
	overlapped := &windows.Overlapped{}
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, overlapped)
}
