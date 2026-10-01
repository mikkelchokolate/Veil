//go:build windows

package backup

import "os"

// restoreChownToMatch is a no-op on Windows (no POSIX ownership).
func restoreChownToMatch(*os.File, int, int) error { return nil }

// fileOwnerIDs: POSIX uid/gid do not exist on Windows.
func fileOwnerIDs(os.FileInfo) (int, int) { return 0, 0 }

// unsafeJournalHardLink: link counts are not expressible through
// os.FileInfo.Sys() on Windows; the pinned-directory checks still apply.
func unsafeJournalHardLink(os.FileInfo) bool { return false }

// unsafeSafetyLinkCount on Windows cannot observe link counts through
// os.FileInfo.Sys(); the nofollow open still rejects swapped symlinks.
func unsafeSafetyLinkCount(os.FileInfo) bool { return false }
