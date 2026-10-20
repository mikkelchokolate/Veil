//go:build !linux && !darwin && !freebsd && !netbsd

package safefs

import "io/fs"

// FileIdentity has no portable dev:ino:ctime source on this platform; callers
// treat the false return as "identity unavailable".
func FileIdentity(fs.FileInfo) (string, bool) { return "", false }
