//go:build linux

package backup

import (
	"fmt"
	"syscall"
)

// backupStatfs is overridable in tests to inject statfs results/failures.
var backupStatfs = syscall.Statfs

// backupFilesystemUsage returns the available bytes and a stable filesystem
// identifier for the filesystem hosting directory.
func backupFilesystemUsage(directory string) (available uint64, filesystemID string, err error) {
	var stats syscall.Statfs_t
	if err := backupStatfs(directory, &stats); err != nil {
		return 0, "", err
	}
	return uint64(stats.Bavail) * uint64(stats.Bsize), fmt.Sprintf("%v", stats.Fsid), nil
}
