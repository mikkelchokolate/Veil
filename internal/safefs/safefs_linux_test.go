//go:build linux

package safefs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// The whole point of the package: a path component swapped for a symlink must
// fail closed instead of being followed to an attacker-chosen target.

func TestOpenDirRejectsSymlinkLeaf(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenDir(link); err == nil {
		t.Fatal("OpenDir followed a symlinked leaf")
	}
}

func TestOpenDirFollowAllowsSymlinkedRoot(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	dir, err := OpenDirFollow(link)
	if err != nil {
		t.Fatalf("OpenDirFollow must accept operator-chosen symlinked roots: %v", err)
	}
	defer dir.Close()
}

func TestOpenDirAtRejectsSwappedSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	dir, err := OpenDir(root)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if err := os.Symlink(outside, filepath.Join(root, "swapped")); err != nil {
		t.Fatal(err)
	}
	if _, err := dir.OpenDirAt("swapped"); err == nil {
		t.Fatal("OpenDirAt followed a symlinked child")
	} else if !errors.Is(err, unix.ELOOP) && !errors.Is(err, unix.ENOTDIR) {
		// O_NOFOLLOW on a symlink yields ELOOP or ENOTDIR depending on
		// kernel order (O_DIRECTORY rejects the unresolved link) — either
		// way the open must fail.
		t.Fatalf("OpenDirAt symlink error = %v, want ELOOP or ENOTDIR", err)
	}
}

func TestStatAtReportsSymlinkNotTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	dir, err := OpenDir(root)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	info, err := dir.StatAt("link")
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("StatAt followed the symlink: mode lacks ModeSymlink")
	}
}

func TestOpenFileAtRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	dir, err := OpenDir(root)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if _, err := dir.OpenFileAt("link"); !errors.Is(err, unix.ELOOP) {
		t.Fatalf("OpenFileAt symlink error = %v, want ELOOP", err)
	}
}

// #1083: a FIFO must be openable without blocking so the caller can fstat and
// reject it — a plain O_RDONLY open would park until a writer appears.
func TestOpenFileAtOnFIFODoesNotBlock(t *testing.T) {
	root := t.TempDir()
	if err := unix.Mkfifo(filepath.Join(root, "planted.fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir, err := OpenDir(root)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	done := make(chan *os.File, 1)
	go func() {
		f, err := dir.OpenFileAt("planted.fifo")
		if err != nil {
			f = nil
		}
		done <- f
	}()
	select {
	case f := <-done:
		if f == nil {
			t.Fatal("OpenFileAt on FIFO returned error")
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&os.ModeNamedPipe == 0 {
			t.Fatalf("FIFO opened but reports mode %v", info.Mode())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OpenFileAt blocked on a FIFO — O_NONBLOCK missing")
	}
}

func TestChmodAtAndChownAtDoNotFollowSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "victim")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "swapped")); err != nil {
		t.Fatal(err)
	}
	dir, err := OpenDir(root)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if err := dir.ChmodAt("swapped", 0o777); err == nil {
		t.Fatal("ChmodAt followed a symlink")
	}
	// ChownAt uses Fchownat(AT_SYMLINK_NOFOLLOW): it changes the LINK itself
	// and must never touch the target. Ownership assertions need CAP_CHOWN,
	// so only verify the target's mode/uid are untouched where possible.
	if err := dir.ChownAt("swapped", os.Getuid(), os.Getgid()); err != nil {
		t.Fatalf("ChownAt on the link itself should succeed: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("chmod leaked through the symlink: mode=%o", info.Mode().Perm())
	}
}

// #1083: ChmodAt on a FIFO must fail fast instead of blocking on open.
func TestChmodAtOnFIFORejectsWithoutBlocking(t *testing.T) {
	root := t.TempDir()
	if err := unix.Mkfifo(filepath.Join(root, "planted.fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir, err := OpenDir(root)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	done := make(chan error, 1)
	go func() { done <- dir.ChmodAt("planted.fifo", 0o600) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("ChmodAt applied a mode to a FIFO")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ChmodAt blocked on a FIFO")
	}
}

func TestCreateRenameRemoveAt(t *testing.T) {
	root := t.TempDir()
	dir, err := OpenDir(root)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	f, name, err := dir.CreateTempAt("tmp-", 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := dir.RenameAt(name, "final"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "final")); err != nil {
		t.Fatal(err)
	}
	// A second exclusive create over the same name must fail.
	if _, err := dir.CreateFileAt("final", 0o600); !errors.Is(err, os.ErrExist) {
		t.Fatalf("CreateFileAt over existing = %v, want EEXIST", err)
	}
	if err := dir.RemoveAt("final"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(root, "final")); !os.IsNotExist(err) {
		t.Fatal("RemoveAt left the entry behind")
	}
}

func TestMkdirAtAndOpenDirAtDescend(t *testing.T) {
	root := t.TempDir()
	dir, err := OpenDir(root)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if err := dir.MkdirAt("sub", 0o750); err != nil {
		t.Fatal(err)
	}
	sub, err := dir.OpenDirAt("sub")
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	if _, err := sub.CreateFileAt("leaf", 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "sub", "leaf")); err != nil {
		t.Fatal(err)
	}
}
