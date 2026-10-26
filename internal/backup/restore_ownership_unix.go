//go:build !windows

package backup

import (
	"fmt"
	"os"
	"syscall"

	"github.com/mikkelchokolate/Veil/internal/safefs"
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
// allowed to trust for ownership (#1219). ok is false when the metadata shape
// carries no ownership: a pinned safefs handle adapts *unix.Stat_t, os.Lstat
// adapts *syscall.Stat_t, and the caller must fail closed on anything else
// instead of turning it into a root-owned chown.
func fileOwnerIDs(info os.FileInfo) (int, int, bool) {
	if uid, gid, ok := safefs.OwnerIDs(info); ok {
		return uid, gid, true
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return int(stat.Uid), int(stat.Gid), true
}

// unsafeJournalHardLink reports whether the entry has extra hard links; a
// multi-linked journal member could alias another file, letting a rename
// leak managed content onto it or a rollback write through it. A metadata
// shape without a link count is treated as unsafe (#1254 review).
func unsafeJournalHardLink(info os.FileInfo) bool {
	count, ok := fileLinkCount(info)
	return !ok || count != 1
}

// unsafeSafetyLinkCount is the stricter counterpart used before overwriting
// a retained safety file: when the link count cannot be determined at all,
// the file is treated as unsafe rather than assumed single-linked.
func unsafeSafetyLinkCount(info os.FileInfo) bool {
	count, ok := fileLinkCount(info)
	return !ok || count != 1
}

// fileLinkCount resolves the hard-link count from either metadata shape.
func fileLinkCount(info os.FileInfo) (uint64, bool) {
	if count, ok := safefs.LinkCount(info); ok {
		return count, true
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint64(stat.Nlink), true
}

// untrustedRestoreJournalSeal reports whether the journal seal key could have
// been read or planted by anyone but this process: it must be owned by the
// effective uid and carry no group/other permission bits (#1256).
func untrustedRestoreJournalSeal(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return true
	}
	return int(stat.Uid) != os.Geteuid() || info.Mode().Perm()&0o077 != 0
}
