//go:build unix

package acmeip

import "syscall"

// fakeSysStat gives fakeFileInfo a real *syscall.Stat_t so the acme.sh trust
// gate and the rollback fileOwnership checks see a deterministic uid/gid
// (#1226).
func fakeSysStat(uid, gid int) any {
	return &syscall.Stat_t{Uid: uint32(uid), Gid: uint32(gid)}
}

// fakeSetOwner mutates the stored uid/gid so a Chown is observable by later
// trust-gate Lstat calls — healing a foreign-owned acme home must stick
// (#1226).
func fakeSetOwner(fi *fakeFileInfo, uid, gid int) {
	st, ok := fi.sys.(*syscall.Stat_t)
	if !ok {
		if fi.sys == nil {
			fi.sys = fakeSysStat(uid, gid)
		}
		return
	}
	st.Uid = uint32(uid)
	st.Gid = uint32(gid)
}
