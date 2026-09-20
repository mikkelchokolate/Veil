//go:build !unix

package managedfiles

import (
	"fmt"
	"os"
)

// ownerOf reports no ownership on platforms without numeric uid/gid in
// FileInfo.Sys; ownership drift checks are skipped there.
func ownerOf(info os.FileInfo) (uid, gid int, ok bool) {
	return 0, 0, false
}

// chownFile fails closed: an ownership contract that cannot be applied must
// not silently produce a root:root file the runtime group cannot read.
func chownFile(path string, uid, gid int) error {
	return fmt.Errorf("chown unsupported on this platform: %s", path)
}
