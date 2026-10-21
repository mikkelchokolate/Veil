//go:build !unix

package uninstall

import "os"

// fileOwnerUID has no portable owner on non-unix platforms; shared runtime
// dirs then require a Veil marker (fail closed).
func fileOwnerUID(os.FileInfo) (uint32, bool) { return 0, false }
