//go:build unix

package api

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// lockSessionStore opens (creating when missing) the sibling lock file of the
// session store at path and takes an exclusive flock on it, blocking until it
// is held. The Panel and the root CLI both hold it around every whole-store
// rewrite and every journal append, so a CLI revocation can no longer land
// between a Panel rewrite's journal read and its journal remove/replace, and
// vice versa (#1265).
//
// The lock file is opened like the journal: relative to the pinned store
// directory, O_NOFOLLOW|O_NONBLOCK, and it must fstat as a single-link
// regular file before a root caller hands it to the directory owner, so a
// planted symlink or hard link cannot turn the open (or the fchown) onto an
// arbitrary file.
func lockSessionStore(path string) (*os.File, error) {
	parent, err := openSessionStoreDir(path)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	name := filepath.Base(sessionStoreLockPath(path))
	fd, err := unix.Openat(int(parent.File().Fd()), name,
		unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open session store lock: %w", err)
	}
	file := os.NewFile(uintptr(fd), sessionStoreLockPath(path))
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("open session store lock %s", sessionStoreLockPath(path))
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || (ok && stat.Nlink != 1) {
		_ = file.Close()
		return nil, fmt.Errorf("session store lock %s is not a single-link regular file", sessionStoreLockPath(path))
	}
	if err := matchSessionStoreOwner(parent.File(), file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("chown session store lock: %w", err)
	}
	for {
		err = unix.Flock(int(file.Fd()), unix.LOCK_EX)
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("lock session store: %w", err)
	}
	return file, nil
}

// unlockSessionStore releases a lock taken by lockSessionStore. Closing the
// descriptor releases the flock even if the explicit unlock fails.
func unlockSessionStore(file *os.File) {
	if file == nil {
		return
	}
	_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
	_ = file.Close()
}
