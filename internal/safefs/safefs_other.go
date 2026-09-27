//go:build !linux

package safefs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Non-Linux fallback: openat/fchownat semantics are unavailable, so the
// operations lstat-check for a symlink and then resolve the path. The
// lstat-then-open window remains, but the only production target is Linux —
// this keeps the package compiling and behaving sensibly elsewhere.

// OpenDir opens path after verifying it is a real directory, not a symlink.
func OpenDir(path string) (*Dir, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("managed path %s must be a real directory", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return &Dir{file: file, path: path}, nil
}

// OpenDirFollow opens path as a directory handle, following symlinks —
// matching the Linux follow-variant for caller-configured roots.
func OpenDirFollow(path string) (*Dir, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}
	if !info.IsDir() {
		return nil, errors.Join(fmt.Errorf("%s is not a directory", path), file.Close())
	}
	return &Dir{file: file, path: path}, nil
}

// OpenDirAt opens the directory child name after an lstat check.
func (d *Dir) OpenDirAt(name string) (*Dir, error) {
	return OpenDir(filepath.Join(d.path, name))
}

// ReadDir lists the directory's entries (unsorted).
func (d *Dir) ReadDir() ([]os.DirEntry, error) {
	return d.file.ReadDir(-1)
}

// StatAt returns lstat-equivalent metadata for child name.
func (d *Dir) StatAt(name string) (os.FileInfo, error) {
	return os.Lstat(filepath.Join(d.path, name))
}

// OpenFileAt opens child name for reading after an lstat symlink check.
func (d *Dir) OpenFileAt(name string) (*os.File, error) {
	return OpenNoFollow(filepath.Join(d.path, name))
}

// CreateFileAt creates child name with O_EXCL so an existing entry fails.
func (d *Dir) CreateFileAt(name string, mode os.FileMode) (*os.File, error) {
	return os.OpenFile(filepath.Join(d.path, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode.Perm())
}

// CreateTempAt creates a uniquely named file inside d.
func (d *Dir) CreateTempAt(prefix string, mode os.FileMode) (*os.File, string, error) {
	file, err := os.CreateTemp(d.path, prefix+"*")
	if err != nil {
		return nil, "", err
	}
	if err := file.Chmod(mode); err != nil {
		name := file.Name()
		_ = file.Close()
		_ = os.Remove(name)
		return nil, "", err
	}
	return file, filepath.Base(file.Name()), nil
}

// ChownAt lchowns the child entry — the link itself, never the target.
func (d *Dir) ChownAt(name string, uid, gid int) error {
	return chownNoFollowPath(filepath.Join(d.path, name), uid, gid)
}

// ChmodAt opens the child after an lstat check and fchmod's the descriptor.
func (d *Dir) ChmodAt(name string, mode os.FileMode) error {
	return chmodNoFollowPath(filepath.Join(d.path, name), mode)
}

// MkdirAt creates child directory name.
func (d *Dir) MkdirAt(name string, mode os.FileMode) error {
	return os.Mkdir(filepath.Join(d.path, name), mode.Perm())
}

// RenameAt renames child oldName to newName within d.
func (d *Dir) RenameAt(oldName, newName string) error {
	return os.Rename(filepath.Join(d.path, oldName), filepath.Join(d.path, newName))
}

// RemoveAt unlinks child name within d.
func (d *Dir) RemoveAt(name string) error {
	return os.Remove(filepath.Join(d.path, name))
}

// OpenNoFollow lstat-checks the leaf for a symlink and then opens it.
func OpenNoFollow(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("managed path must not be a symlink")
	}
	return os.Open(path)
}

// ChmodNoFollow applies mode on an lstat-checked descriptor.
func ChmodNoFollow(path string, mode os.FileMode) error {
	return chmodNoFollowPath(path, mode)
}

// ChownNoFollow is the ownership counterpart of ChmodNoFollow.
func ChownNoFollow(path string, uid, gid int) error {
	return chownNoFollowPath(path, uid, gid)
}

func chmodNoFollowPath(path string, mode os.FileMode) error {
	file, err := OpenNoFollow(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Chmod(mode)
}

func chownNoFollowPath(path string, uid, gid int) error {
	file, err := OpenNoFollow(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Chown(uid, gid)
}
