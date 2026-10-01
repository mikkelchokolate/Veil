//go:build windows

package acmeip

import (
	"os"
	"os/exec"
)

func configureProcessGroup(cmd *exec.Cmd) {}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}

// fileOwnership is a no-op stub — Windows os.FileInfo.Sys exposes no uid/gid,
// so snapshot/restore keeps bytes+mode and skips chown, as fixCertOwnership
// already does.
func fileOwnership(fi os.FileInfo) (uid, gid int, ok bool) {
	return 0, 0, false
}

func fileOwnedByUID(fi os.FileInfo, uid int) bool { return false }
