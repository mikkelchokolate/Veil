//go:build !linux

package backup

import "errors"

// backupFilesystemUsage has no portable statfs on this platform; space
// preflight reports unsupported rather than silently skipping the policy.
func backupFilesystemUsage(string) (available uint64, filesystemID string, err error) {
	return 0, "", errors.New("backup space preflight is unsupported on this platform")
}
