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
//
// The chown runs on the still-open staged descriptor — never on a path that
// a service-writable directory could have swapped for a symlink in between
// (#1219).
func restoreChownToMatch(staged *os.File, uid, gid int) error {
	if os.Geteuid() != 0 {
		return nil
	}
	if err := staged.Chown(uid, gid); err != nil {
		return fmt.Errorf("preserve ownership on restore: %w", err)
	}
	return nil
}

// fileOwnerIDs extracts uid/gid from lstat-style metadata captured on a
// pinned descriptor or via lstat — the only provenance a journal/restore is
// allowed to trust for ownership (#1219).
func fileOwnerIDs(info os.FileInfo) (int, int) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0
	}
	return int(stat.Uid), int(stat.Gid)
}

// unsafeJournalHardLink reports whether the entry has extra hard links; a
// multi-linked journal member could alias another file, letting a rename
// leak managed content onto it or a rollback write through it.
func unsafeJournalHardLink(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink != 1
}

// unsafeSafetyLinkCount is the stricter counterpart used before overwriting
// a retained safety file: when the link count cannot be determined at all,
// the file is treated as unsafe rather than assumed single-linked.
func unsafeSafetyLinkCount(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return !ok || stat.Nlink != 1
}
