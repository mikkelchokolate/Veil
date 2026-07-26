package managementstate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const snapshotBarrierFilename = ".veil-snapshot.lock"

// WithSnapshotBarrier serializes a management-state commit (state file plus
// desired revision/snapshot) with a backup capture performed by another
// process, such as the systemd backup timer. The lock file contains no data;
// its permissive mode is safe because the containing state directory remains
// restricted, and it lets both the veil service user and root-run recovery
// commands participate in the same advisory lock.
func WithSnapshotBarrier(statePath string, fn func() error) error {
	if fn == nil {
		return errors.New("snapshot barrier callback is required")
	}
	if statePath == "" {
		return fn()
	}
	directory := filepath.Dir(statePath)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create management snapshot barrier directory: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(directory, snapshotBarrierFilename), os.O_CREATE|os.O_RDWR, 0o666)
	if err != nil {
		return fmt.Errorf("open management snapshot barrier: %w", err)
	}
	defer file.Close()
	if info, statErr := file.Stat(); statErr == nil && info.Mode().Perm() != 0o666 {
		if err := file.Chmod(0o666); err != nil {
			return fmt.Errorf("set management snapshot barrier mode: %w", err)
		}
	}
	for {
		err = unix.Flock(int(file.Fd()), unix.LOCK_EX)
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	if err != nil {
		return fmt.Errorf("lock management snapshot barrier: %w", err)
	}
	defer func() { _ = unix.Flock(int(file.Fd()), unix.LOCK_UN) }()
	return fn()
}
