package managementstate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mikkelchokolate/Veil/internal/safefs"
)

const snapshotBarrierFilename = ".veil-snapshot.lock"

// WithSnapshotBarrier serializes a management-state commit (state file plus
// desired revision/snapshot) with a backup capture performed by another
// process, such as the systemd backup timer. The lock file contains no data;
// its permissive mode is safe because the containing state directory remains
// restricted, and it lets both the veil service user and root-run recovery
// commands participate in the same advisory lock.
//
// The state directory is writable by the unprivileged service account, so no
// path under it is ever re-resolved for the lock: the directory is pinned on a
// descriptor (O_NOFOLLOW|O_DIRECTORY) and the lock leaf is opened relative to
// it with O_NOFOLLOW|O_NONBLOCK, then fstat-verified. A planted symlink fails
// ELOOP at open and a swapped FIFO cannot block, instead of a path-based
// open/chmod following into a root-owned target (#1216). The file's mode is
// only ever set on the descriptor of a file this code just created with
// O_EXCL; an existing lock is trusted because hostaccess pre-creates it with a
// fixed owner and mode, so runtime never needs to chmod an inode that might
// not be ours.
func WithSnapshotBarrier(statePath string, fn func() error) (resultErr error) {
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
	stateDir, err := safefs.OpenDir(directory)
	if err != nil {
		return fmt.Errorf("open management snapshot barrier directory: %w", err)
	}
	defer func() {
		resultErr = errors.Join(resultErr, stateDir.Close())
	}()
	file, err := openSnapshotBarrierLock(stateDir)
	if err != nil {
		return fmt.Errorf("open management snapshot barrier: %w", err)
	}
	defer func() {
		resultErr = errors.Join(resultErr, releaseSnapshotBarrier(file))
	}()
	if err := snapshotBarrierLock(file); err != nil {
		return fmt.Errorf("lock management snapshot barrier: %w", err)
	}
	return fn()
}

// openSnapshotBarrierLock opens (or, when absent, exclusively creates) the
// barrier leaf inside the pinned state directory and fstat-verifies the
// opened inode: it must be a regular file with no extra hard links.
func openSnapshotBarrierLock(dir *safefs.Dir) (*os.File, error) {
	file, err := dir.OpenFileAtRW(snapshotBarrierFilename)
	if errors.Is(err, os.ErrNotExist) {
		file, err = createSnapshotBarrierLock(dir)
	}
	if err != nil {
		return nil, err
	}
	if err := verifySnapshotBarrierFile(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func createSnapshotBarrierLock(dir *safefs.Dir) (*os.File, error) {
	file, err := dir.CreateFileAtRW(snapshotBarrierFilename, 0o666)
	if errors.Is(err, os.ErrExist) {
		// Another participant created the lock between our open and create;
		// the O_EXCL guarantee still held for whichever inode won.
		return dir.OpenFileAtRW(snapshotBarrierFilename)
	}
	if err != nil {
		return nil, err
	}
	// fchmod on the descriptor of the inode O_EXCL just bound to us — safe
	// even inside the service-writable directory because the name is never
	// re-resolved and the create cannot have followed anything.
	if err := file.Chmod(0o666); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}

func verifySnapshotBarrierFile(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("management snapshot barrier is not a regular file")
	}
	return verifySnapshotBarrierLinks(info)
}

func releaseSnapshotBarrier(file *os.File) error {
	unlockErr := snapshotBarrierUnlock(file)
	closeErr := file.Close()
	if unlockErr != nil {
		unlockErr = fmt.Errorf("unlock management snapshot barrier: %w", unlockErr)
	}
	if closeErr != nil {
		closeErr = fmt.Errorf("close management snapshot barrier: %w", closeErr)
	}
	return errors.Join(unlockErr, closeErr)
}
