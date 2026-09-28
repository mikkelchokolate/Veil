//go:build windows

package managementstate

// Windows directory handles cannot be flushed the way POSIX fsync(dirfd)
// works (FlushFileBuffers fails on them), so directory sync is a no-op; the
// file-level Sync in writeStoreFileAtomicWithSync is the durability signal
// Windows can express (issue #1085).
func syncStoreDirectory(string) error { return nil }
