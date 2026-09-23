//go:build windows

package atomicfile

// preserveOwner is a no-op on Windows: uid/gid ownership does not exist and
// the managed-files contract is POSIX-only.
func preserveOwner(string, string) error { return nil }
