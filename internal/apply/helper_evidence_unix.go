//go:build unix

package apply

import (
	"fmt"
	"io/fs"
	"syscall"
)

// helperEvidenceDirRootControlled reports whether the directory holding an
// activation evidence file is root-owned with no group/other write bits — the
// helper evidence contract requires a directory only root can write.
func helperEvidenceDirRootControlled(info fs.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == 0 && info.Mode().Perm()&0o022 == 0
}

// helperEvidenceFileRootOwned reports whether an evidence file is a
// single-link root-owned node — hardlinked or non-root evidence is not the
// immutable helper-committed object the recovery path requires.
func helperEvidenceFileRootOwned(info fs.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == 0 && stat.Nlink == 1
}

// installedPathInodeTag formats the dev:ino:ctime identity helper evidence
// records for the installed binary, so a swapped binary cannot reuse the
// activation manifest. The second return is false when the platform cannot
// supply an inode identity — callers then skip the comparison.
func installedPathInodeTag(info fs.FileInfo) (string, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("%d:%d:%d:%d", stat.Dev, stat.Ino, stat.Ctim.Sec, stat.Ctim.Nsec), true
}
