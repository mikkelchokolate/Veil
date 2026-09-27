//go:build linux

package safefs

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

// dirOpenFlags pins a directory descriptor to the inode that exists at open
// time: O_NOFOLLOW rejects a leaf symlink (ELOOP) and O_DIRECTORY rejects
// non-directories. Once held, no path resolution can redirect operations on
// its children.
const dirOpenFlags = unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_DIRECTORY

// fileOpenFlags opens a leaf for reading without following a swapped symlink
// and without blocking on special files: O_NONBLOCK makes a FIFO open return
// immediately instead of parking the caller until a writer appears (#1083).
// Callers must fstat the result and reject non-regular/non-directory types.
const fileOpenFlags = unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK

// OpenDir opens path as a pinned directory handle. A symlinked or
// non-directory leaf is rejected (ELOOP / ENOTDIR).
func OpenDir(path string) (*Dir, error) {
	fd, err := unix.Open(path, dirOpenFlags, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("open managed directory %s", path)
	}
	return &Dir{file: file, path: path}, nil
}

// OpenDirFollow opens path as a pinned directory handle but follows symlinks
// in every component, leaf included. Use it for caller-configured roots where
// a symlink is legitimate operator choice (e.g. a backup dir parented through
// an admin-managed link): the returned handle still pins the resolved inode,
// so any later swap of the path cannot redirect operations on its children.
func OpenDirFollow(path string) (*Dir, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("open directory %s", path)
	}
	return &Dir{file: file, path: path}, nil
}

// OpenDirAt opens the directory child name relative to d.
func (d *Dir) OpenDirAt(name string) (*Dir, error) {
	fd, err := unix.Openat(int(d.file.Fd()), name, dirOpenFlags, 0)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(d.path, name)
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("open managed directory %s", path)
	}
	return &Dir{file: file, path: path}, nil
}

// ReadDir lists the directory's entries (unsorted). The returned DirEntry
// type bits are hints only — authoritative metadata must come from StatAt,
// which is descriptor-pinned.
func (d *Dir) ReadDir() ([]os.DirEntry, error) {
	return d.file.ReadDir(-1)
}

// StatAt returns lstat-equivalent metadata for child name resolved relative
// to d (Fstatat + AT_SYMLINK_NOFOLLOW): a swapped symlink reports as a
// symlink, never as its target.
func (d *Dir) StatAt(name string) (os.FileInfo, error) {
	var stat unix.Stat_t
	if err := unix.Fstatat(int(d.file.Fd()), name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return nil, err
	}
	return statFileInfo{name: name, stat: stat}, nil
}

// OpenFileAt opens child name for reading relative to d with O_NOFOLLOW and
// O_NONBLOCK. Special files must be rejected by the caller via fstat: a FIFO
// opens successfully here but reads as EAGAIN, a socket still fails ENXIO.
func (d *Dir) OpenFileAt(name string) (*os.File, error) {
	fd, err := unix.Openat(int(d.file.Fd()), name, fileOpenFlags, 0)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(d.path, name)
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("open managed path %s", path)
	}
	return file, nil
}

// CreateFileAt creates child name relative to d with O_WRONLY|O_CREAT|
// O_EXCL|O_NOFOLLOW. An existing entry (file or symlink) fails EEXIST, so the
// create can never clobber or follow attacker-planted material.
func (d *Dir) CreateFileAt(name string, mode os.FileMode) (*os.File, error) {
	fd, err := unix.Openat(int(d.file.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, uint32(mode.Perm()))
	if err != nil {
		return nil, err
	}
	path := filepath.Join(d.path, name)
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("create managed file %s", path)
	}
	return file, nil
}

// CreateTempAt creates a uniquely named file inside d (prefix + random hex)
// with O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW, returning the file and the chosen
// leaf name. Use with RenameAt for atomic temp+publish inside a pinned
// directory.
func (d *Dir) CreateTempAt(prefix string, mode os.FileMode) (*os.File, string, error) {
	buf := make([]byte, 8)
	for i := 0; i < 10000; i++ {
		if _, err := rand.Read(buf); err != nil {
			return nil, "", err
		}
		name := prefix + hex.EncodeToString(buf)
		file, err := d.CreateFileAt(name, mode)
		if err == nil {
			return file, name, nil
		}
		if !os.IsExist(err) {
			return nil, "", err
		}
	}
	return nil, "", fmt.Errorf("create temp file in %s: too many collisions", d.path)
}

// ChownAt changes ownership of child name with Fchownat +
// AT_SYMLINK_NOFOLLOW. The descriptor is never opened, so symlinks are never
// followed and special files (FIFOs, sockets) chown without blocking —
// matching `chown -R` semantics (#1083).
func (d *Dir) ChownAt(name string, uid, gid int) error {
	return unix.Fchownat(int(d.file.Fd()), name, uid, gid, unix.AT_SYMLINK_NOFOLLOW)
}

// ChmodAt changes the mode of child name. Linux fchmodat does not implement
// AT_SYMLINK_NOFOLLOW, so the entry is opened O_NOFOLLOW|O_NONBLOCK relative
// to d and fchmod'ed on the descriptor; special files opened by a mid-flight
// swap are rejected before chmod can touch them (#1083).
func (d *Dir) ChmodAt(name string, mode os.FileMode) error {
	file, err := d.OpenFileAt(name)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		return fmt.Errorf("refuse to chmod non-regular managed path %s", filepath.Join(d.path, name))
	}
	return file.Chmod(mode)
}

// MkdirAt creates child directory name relative to d.
func (d *Dir) MkdirAt(name string, mode os.FileMode) error {
	return unix.Mkdirat(int(d.file.Fd()), name, uint32(mode.Perm()))
}

// RenameAt renames child oldName to newName within d.
func (d *Dir) RenameAt(oldName, newName string) error {
	return unix.Renameat(int(d.file.Fd()), oldName, int(d.file.Fd()), newName)
}

// RemoveAt unlinks child name within d (non-directory entries only).
func (d *Dir) RemoveAt(name string) error {
	return unix.Unlinkat(int(d.file.Fd()), name, 0)
}

// OpenNoFollow opens path for reading with O_NOFOLLOW|O_NONBLOCK. The
// descriptor is pinned to the leaf inode present at open time; callers must
// fstat and reject unexpected file types.
func OpenNoFollow(path string) (*os.File, error) {
	fd, err := unix.Open(path, fileOpenFlags, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("open managed path %s", path)
	}
	return file, nil
}

// ChmodNoFollow applies mode to path on an O_NOFOLLOW-opened descriptor so a
// symlink swapped in after the caller's check is rejected (ELOOP) rather
// than followed. Non-regular/non-directory types (FIFO, socket) are
// rejected after the non-blocking open instead of hanging (#1083).
func ChmodNoFollow(path string, mode os.FileMode) error {
	file, err := OpenNoFollow(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		return fmt.Errorf("refuse to chmod non-regular managed path %s", path)
	}
	return file.Chmod(mode)
}

// ChownNoFollow is the ownership counterpart of ChmodNoFollow: fchown on an
// O_NOFOLLOW|O_NONBLOCK-opened descriptor never follows a swapped symlink
// and never blocks on a FIFO.
func ChownNoFollow(path string, uid, gid int) error {
	file, err := OpenNoFollow(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		return fmt.Errorf("refuse to chown non-regular managed path %s", path)
	}
	return file.Chown(uid, gid)
}

// statFileInfo adapts unix.Stat_t to fs.FileInfo.
type statFileInfo struct {
	name string
	stat unix.Stat_t
}

func (i statFileInfo) Name() string { return i.name }
func (i statFileInfo) Size() int64  { return i.stat.Size }
func (i statFileInfo) Mode() fs.FileMode {
	m := fs.FileMode(i.stat.Mode & 0o777)
	switch i.stat.Mode & unix.S_IFMT {
	case unix.S_IFDIR:
		m |= fs.ModeDir
	case unix.S_IFLNK:
		m |= fs.ModeSymlink
	case unix.S_IFIFO:
		m |= fs.ModeNamedPipe
	case unix.S_IFSOCK:
		m |= fs.ModeSocket
	case unix.S_IFBLK:
		m |= fs.ModeDevice
	case unix.S_IFCHR:
		m |= fs.ModeDevice | fs.ModeCharDevice
	}
	if i.stat.Mode&unix.S_ISUID != 0 {
		m |= fs.ModeSetuid
	}
	if i.stat.Mode&unix.S_ISGID != 0 {
		m |= fs.ModeSetgid
	}
	if i.stat.Mode&unix.S_ISVTX != 0 {
		m |= fs.ModeSticky
	}
	return m
}
func (i statFileInfo) ModTime() time.Time {
	return time.Unix(i.stat.Mtim.Sec, i.stat.Mtim.Nsec)
}
func (i statFileInfo) IsDir() bool { return i.Mode().IsDir() }
func (i statFileInfo) Sys() any    { return &i.stat }
