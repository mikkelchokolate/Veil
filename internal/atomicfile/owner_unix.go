//go:build !windows

package atomicfile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

// chownFile/geteuid/statFile are test hooks so ownership preservation can be
// exercised without relying on real account state or privileges.
var (
	chownFile = os.Chown
	geteuid   = os.Geteuid
	statFile  = os.Stat
)

// preserveOwner copies the replaced file's uid/gid onto the staged temp file
// before the atomic rename. Rewriting a managed file would otherwise expose a
// root-owned window to readers that relied on the previous group ownership —
// e.g. veil-proxy protocol units re-reading tls certs mid-reinstall while a
// reloadcmd restarts them (install-acceptance flake: hy2 died on
// tls.crt permission denied between WriteFile and the later chown pass).
// Non-root writers cannot chown; their temp file already carries the writer's
// uid, which is the only ownership they could have produced anyway.
//
// Only a missing target skips preservation (new file — nothing to preserve).
// Any other Stat failure (EACCES, IO errors, …) aborts the write before
// rename: soft-succeeding would re-open the root-owned window for a file that
// does exist.
func preserveOwner(tmpPath, target string) error {
	if geteuid() != 0 {
		return nil
	}
	info, err := statFile(target)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil // new file — nothing to preserve
		}
		return fmt.Errorf("stat %s before preserving owner: %w", target, err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("stat %s: cannot determine owner from %T", target, info.Sys())
	}
	return chownFile(tmpPath, int(st.Uid), int(st.Gid))
}
