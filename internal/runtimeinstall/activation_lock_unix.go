//go:build unix

package runtimeinstall

import (
	"os"

	"golang.org/x/sys/unix"
)

// runtimeActivationLock takes the exclusive activation lock on file, blocking
// until it is held (flock LOCK_EX semantics).
func runtimeActivationLock(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX)
}

func runtimeActivationUnlock(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}
