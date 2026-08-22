//go:build windows

package backup

import "os"

// restoreChownToMatch is a no-op on Windows (no POSIX ownership).
func restoreChownToMatch(string, os.FileInfo) error { return nil }
