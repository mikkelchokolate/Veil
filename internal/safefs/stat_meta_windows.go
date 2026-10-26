//go:build windows

package safefs

import "io/fs"

// OwnerIDs: POSIX ownership does not exist on Windows.
func OwnerIDs(fs.FileInfo) (int, int, bool) { return 0, 0, false }

// LinkCount: link counts are not expressible through os.FileInfo on Windows.
func LinkCount(fs.FileInfo) (uint64, bool) { return 0, false }
