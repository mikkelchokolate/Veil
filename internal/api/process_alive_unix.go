//go:build unix

package api

import (
	"errors"
	"syscall"
)

// processAlive probes pid with signal 0; EPERM still proves the process
// exists (it just belongs to another uid).
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
