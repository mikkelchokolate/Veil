package privileged

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"

	"github.com/mikkelchokolate/Veil/internal/safefs"
)

// openLockFileAt opens or creates name inside the pinned directory with
// O_NOFOLLOW: an attacker-planted symlink leaf is rejected instead of being
// followed into an arbitrary root-opened file (#1229-F5). The parent is
// pinned on the descriptor so a swapped ancestor cannot redirect the create.
func openLockFileAt(dir *safefs.Dir, name string) (*os.File, error) {
	fd, err := unix.Openat(int(dir.File().Fd()), name,
		unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), filepath.Join(dir.Path(), name))
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("open lock file %s in %s", name, dir.Path())
	}
	return file, nil
}

func releaseLockedFile(file *os.File) error {
	unlockErr := unix.Flock(int(file.Fd()), unix.LOCK_UN)
	closeErr := file.Close()
	if unlockErr != nil {
		unlockErr = fmt.Errorf("unlock file: %w", unlockErr)
	}
	if closeErr != nil {
		closeErr = fmt.Errorf("close file: %w", closeErr)
	}
	return errors.Join(unlockErr, closeErr)
}
