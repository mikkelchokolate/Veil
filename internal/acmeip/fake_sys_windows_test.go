//go:build windows

package acmeip

// fakeSysStat returns nil on Windows: FileInfo.Sys exposes no uid/gid, so
// ownership checks in the trust gate and rollback stay skipped the same way
// they are on a real Windows filesystem (#1226).
func fakeSysStat(uid, gid int) any { return nil }

// fakeSetOwner is a no-op on Windows — FileInfo.Sys exposes no uid/gid there,
// matching the real filesystem the ownership checks skip.
func fakeSetOwner(fi *fakeFileInfo, uid, gid int) {}
