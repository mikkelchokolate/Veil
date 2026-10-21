//go:build unix

package api

import (
	"os"
	"syscall"
)

var (
	sessionStoreEUID  = os.Geteuid
	sessionStoreChown = func(file *os.File, uid, gid int) error { return file.Chown(uid, gid) }
)

// matchSessionStoreOwner hands a session store file written by root (the CLI
// revocation paths, #1265) to the owner of the store directory, so the
// unprivileged Panel can still read, append to and replace it. dir and file
// are both descriptors, so nothing is re-resolved by path. A non-root caller
// creates files it already owns and skips this.
func matchSessionStoreOwner(dir, file *os.File) error {
	if sessionStoreEUID() != 0 {
		return nil
	}
	info, err := dir.Stat()
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	return sessionStoreChown(file, int(stat.Uid), int(stat.Gid))
}
