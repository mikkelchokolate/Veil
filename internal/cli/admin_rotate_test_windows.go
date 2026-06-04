//go:build windows

package cli

import (
	"syscall"
)

func isFailureSimulationSupported() bool {
	return true
}

func lockStateFileForRenameFailure(path string) (func(), error) {
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	// Open the file with GENERIC_READ, but FILE_SHARE_READ sharing mode.
	// This allows other processes to read it (so store.Load succeeds),
	// but prevents writing, deleting, or renaming it (so os.Rename fails).
	handle, err := syscall.CreateFile(
		pathPtr,
		syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, err
	}

	unlock := func() {
		_ = syscall.CloseHandle(handle)
	}
	return unlock, nil
}
