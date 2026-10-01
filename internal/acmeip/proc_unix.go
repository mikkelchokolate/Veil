//go:build unix

package acmeip

import (
	"os"
	"os/exec"
	"syscall"
)

func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

// fileOwnership extracts the owning uid/gid so the installcert rollback can
// restore the predecessor's metadata exactly (#1208). ok is false when the
// platform does not expose uid/gid through FileInfo.Sys.
func fileOwnership(fi os.FileInfo) (uid, gid int, ok bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return int(st.Uid), int(st.Gid), true
}

// fileOwnedByUID reports whether the file is owned by uid. Callers gate it
// on getuidFunc() == 0 the same way fixCertOwnership gates its chown: only a
// root caller can trust a uid check.
func fileOwnedByUID(fi os.FileInfo, uid int) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	return int(st.Uid) == uid
}
