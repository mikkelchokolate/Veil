//go:build !unix

package privileged

import "io/fs"

// fileOwnerUID has no uid model off-Unix; ownership checks fail closed.
func fileOwnerUID(fs.FileInfo) (uint32, bool) { return 0, false }
