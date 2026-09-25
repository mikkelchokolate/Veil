//go:build !unix

package apply

import "io/fs"

// Non-unix platforms have no uid-0/nlink ownership contract, so helper
// evidence can never be verified there. Fail closed — report every check as
// failed rather than accepting unverifiable evidence (issue #1011).
func helperEvidenceDirRootControlled(fs.FileInfo) bool { return false }
func helperEvidenceFileRootOwned(fs.FileInfo) bool     { return false }
func installedPathInodeTag(fs.FileInfo) (string, bool) { return "", false }
