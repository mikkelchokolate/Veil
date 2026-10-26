//go:build unix

package safefs

import (
	"io/fs"

	"golang.org/x/sys/unix"
)

// OwnerIDs returns the POSIX owner of metadata captured by StatAt or an open
// descriptor. FileInfo from os.Lstat carries *syscall.Stat_t and is decoded by
// the caller instead; this only understands the adapter's *unix.Stat_t, so an
// unknown shape reports false rather than a zero owner.
func OwnerIDs(info fs.FileInfo) (uid, gid int, ok bool) {
	stat, ok := info.Sys().(*unix.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return int(stat.Uid), int(stat.Gid), true
}

// LinkCount returns the hard-link count of safefs-captured metadata. A shape
// this package cannot decode reports false, which callers treat as unsafe.
func LinkCount(info fs.FileInfo) (uint64, bool) {
	stat, ok := info.Sys().(*unix.Stat_t)
	if !ok {
		return 0, false
	}
	return uint64(stat.Nlink), true
}
