//go:build unix

package managementstate

import (
	"os"
	"syscall"
)

// fileOwnerUID/fileOwnerGID read the numeric owner out of a stat result so an
// atomic state rewrite can preserve ownership. A non-Stat_t Sys() reports -1,
// which disables chown preservation for that file.
func fileOwnerUID(fi os.FileInfo) int {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return int(st.Uid)
	}
	return -1
}

func fileOwnerGID(fi os.FileInfo) int {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return int(st.Gid)
	}
	return -1
}
