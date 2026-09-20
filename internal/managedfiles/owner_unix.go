//go:build unix

package managedfiles

import (
	"os"
	"syscall"
)

// ownerOf extracts the numeric owner of an existing file. The second return
// reports whether the platform exposes uid/gid through FileInfo.Sys.
func ownerOf(info os.FileInfo) (uid, gid int, ok bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return int(stat.Uid), int(stat.Gid), true
}

// chownFile applies the numeric ownership contract to a managed file.
func chownFile(path string, uid, gid int) error {
	return os.Chown(path, uid, gid)
}
