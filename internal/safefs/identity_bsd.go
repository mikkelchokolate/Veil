//go:build darwin || freebsd || netbsd

package safefs

import (
	"fmt"
	"io/fs"
	"syscall"
)

// FileIdentity formats the dev:ino:ctime identity of info. BSD-derived
// kernels expose the change time as Stat_t.Ctimespec rather than Linux's
// Ctim (#1243).
func FileIdentity(info fs.FileInfo) (string, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("%d:%d:%d:%d", stat.Dev, stat.Ino, stat.Ctimespec.Sec, stat.Ctimespec.Nsec), true
}
