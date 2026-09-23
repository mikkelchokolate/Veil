//go:build !windows

package atomicfile

import (
	"os"
	"syscall"
)

// chownFile/geteuid are test hooks so ownership preservation can be exercised
// without relying on real account state or privileges.
var (
	chownFile = os.Chown
	geteuid   = os.Geteuid
)

// preserveOwner copies the replaced file's uid/gid onto the staged temp file
// before the atomic rename. Rewriting a managed file would otherwise expose a
// root-owned window to readers that relied on the previous group ownership —
// e.g. veil-proxy protocol units re-reading tls certs mid-reinstall while a
// reloadcmd restarts them (install-acceptance flake: hy2 died on
// tls.crt permission denied between WriteFile and the later chown pass).
// Non-root writers cannot chown; their temp file already carries the writer's
// uid, which is the only ownership they could have produced anyway.
func preserveOwner(tmpPath, target string) error {
	if geteuid() != 0 {
		return nil
	}
	info, err := os.Stat(target)
	if err != nil {
		return nil // new file — nothing to preserve
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	return chownFile(tmpPath, int(st.Uid), int(st.Gid))
}
