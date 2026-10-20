//go:build unix

package privileged

import (
	"io/fs"
	"syscall"
)

// fileOwnerUID returns the owning uid of info; false when the platform stat
// payload is not a syscall.Stat_t.
func fileOwnerUID(info fs.FileInfo) (uint32, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return stat.Uid, true
}
