// Package safefs performs filesystem operations without following
// attacker-swappable path components.
//
// Root-owned maintenance passes run over trees owned by unprivileged
// service accounts. A path resolved by name is only as safe as every
// intermediate component: between a walker's lstat and the chmod/chown/
// open, an entry swapped for a symlink — or a directory ancestor swapped
// for one — redirects the operation anywhere on the filesystem (#1009,
// #1127). On Linux this package holds directory descriptors open and
// resolves every child operation (stat, open, chmod, chown, mkdir, rename)
// relative to the parent's file descriptor with O_NOFOLLOW /
// AT_SYMLINK_NOFOLLOW, so no component of the working path is ever
// re-resolved against a mutating filesystem.
//
// Platforms without openat/fchownat semantics fall back to
// lstat-check-then-open, which is compile-compatible but racy — production
// installs run on Linux only.
package safefs

import (
	"fmt"
	"os"
)

// Dir is a directory handle held open for the lifetime of a managed-tree
// operation. On Linux all child operations are resolved relative to the
// descriptor (fd-pinned); Path exists for error messages and for the
// non-Linux fallback only — it must never be re-resolved for mutations.
type Dir struct {
	file *os.File
	path string
}

// Path returns the display path the handle was opened under. It is for
// error messages only.
func (d *Dir) Path() string { return d.path }

// File returns the underlying open directory descriptor.
func (d *Dir) File() *os.File { return d.file }

// Close releases the directory handle.
func (d *Dir) Close() error { return d.file.Close() }

// removeTreeAtMaxDepth bounds recursive tree removal inside a pinned
// directory — far past any legitimate managed-tree depth — so a pathological
// nesting cannot exhaust the stack.
const removeTreeAtMaxDepth = 64

// RemoveTreeAt recursively removes the child tree rooted at name, resolving
// every step relative to the pinned directory descriptors. Unlike
// os.RemoveAll on a path, a mid-flight swap cannot redirect the removal:
// symlinks encountered anywhere in the tree are unlinked as symlinks and
// never followed, and a directory ancestor is only descended through an
// OpenDirAt handle already bound to its inode (#1219).
func (d *Dir) RemoveTreeAt(name string) error {
	return d.removeTreeAt(name, 0)
}

func (d *Dir) removeTreeAt(name string, depth int) error {
	if depth > removeTreeAtMaxDepth {
		return fmt.Errorf("managed tree at %s exceeds depth limit", d.path)
	}
	info, err := d.StatAt(name)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return d.RemoveAt(name)
	}
	child, err := d.OpenDirAt(name)
	if err != nil {
		return err
	}
	entries, readErr := child.ReadDir()
	if readErr != nil {
		_ = child.Close()
		return readErr
	}
	for _, entry := range entries {
		if err := child.removeTreeAt(entry.Name(), depth+1); err != nil {
			_ = child.Close()
			return err
		}
	}
	if err := child.Close(); err != nil {
		return err
	}
	return d.RemoveDirAt(name)
}
