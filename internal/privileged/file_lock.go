package privileged

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

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
