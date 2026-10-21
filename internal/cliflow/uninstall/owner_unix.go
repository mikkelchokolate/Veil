//go:build unix

package uninstall

import (
	"os"
	"syscall"
)

func fileOwnerUID(info os.FileInfo) (uint32, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return st.Uid, true
}
