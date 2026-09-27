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

import "os"

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
