//go:build linux

package safefs

import (
	"fmt"
	"io/fs"
	"syscall"
)

// FileIdentity formats the dev:ino:ctime identity of info, so a file swapped
// in under the same name (new inode, or same inode with a new ctime) is
// distinguishable from the one originally recorded. The second return is
// false when the platform cannot supply an inode identity.
func FileIdentity(info fs.FileInfo) (string, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("%d:%d:%d:%d", stat.Dev, stat.Ino, stat.Ctim.Sec, stat.Ctim.Nsec), true
}
