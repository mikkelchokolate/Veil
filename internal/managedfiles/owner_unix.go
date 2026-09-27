//go:build unix

package managedfiles

import (
	"os"
	"syscall"

	"github.com/mikkelchokolate/Veil/internal/safefs"
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

// chownFile applies the numeric ownership contract to a managed file on an
// O_NOFOLLOW-pinned descriptor: between atomicfile.Write's rename and this
// call an attacker who controls the file's parent directory can swap the
// leaf for a symlink, and a path-based os.Chown would follow it to an
// arbitrary victim (#1129).
func chownFile(path string, uid, gid int) error {
	return safefs.ChownNoFollow(path, uid, gid)
}
