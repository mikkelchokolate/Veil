//go:build windows

package apply

import (
	"errors"

	"golang.org/x/sys/windows"
)

// processAlive reports whether pid names a live process, mirroring the unix
// signal-0 probe: OpenProcess with the minimal query right fails for dead
// pids, while ERROR_ACCESS_DENIED means the process exists but is owned by
// another identity — still alive, matching the EPERM branch on unix.
func processAlive(pid int) bool {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	_ = windows.CloseHandle(handle)
	return true
}
