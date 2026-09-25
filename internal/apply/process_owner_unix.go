//go:build unix

package apply

import (
	"errors"
	"syscall"
)

// processAlive reports whether pid names a live process. Signal 0 performs
// the existence check only; EPERM means the process exists but belongs to a
// user we cannot signal — still alive (issue #1011).
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
