//go:build unix

package managementstate

import "os"

// syncStoreDirectory fsyncs the containing directory so the rename that
// published the state file survives a crash (issue #1085 — split out so the
// package cross-compiles; Windows has no directory-fsync equivalent).
func syncStoreDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
