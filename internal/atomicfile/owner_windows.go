//go:build windows

package atomicfile

import "os"

// preserveOwner is a no-op on Windows: uid/gid ownership does not exist and
// the managed-files contract is POSIX-only.
func preserveOwner(*os.File, string) error { return nil }
