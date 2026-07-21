//go:build !windows

package backup

import (
	"fmt"
	"os"
	"syscall"
)

// restoreChownToMatch aligns the staged replacement's owner/group with the
// file being replaced. Only attempted as root: an unprivileged restore (CLI
// run by the state owner) already creates files with the right identity, and
// a chown would fail with EPERM. As root (privileged helper), leaving the
// replacement root-owned would lock the unprivileged panel out of its own
// state/key after restore.
func restoreChownToMatch(path string, original os.FileInfo) error {
	if os.Geteuid() != 0 {
		return nil
	}
	stat, ok := original.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	if err := os.Chown(path, int(stat.Uid), int(stat.Gid)); err != nil {
		return fmt.Errorf("preserve ownership on restore: %w", err)
	}
	return nil
}
