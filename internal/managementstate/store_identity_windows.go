//go:build windows

package managementstate

import "os"

// Windows file metadata carries no uid/gid; -1 disables ownership
// preservation (matching the non-Stat_t path on unix, issue #1085).
func fileOwnerUID(os.FileInfo) int { return -1 }

func fileOwnerGID(os.FileInfo) int { return -1 }
