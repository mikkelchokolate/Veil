//go:build !unix

package api

import "os"

// processAlive reports whether pid names a live process. Off-Unix
// os.FindProcess opens a process handle and fails for a pid that does not
// exist (#1243: syscall.Kill is Unix-only and broke the Windows build).
func processAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = process.Release()
	return true
}
