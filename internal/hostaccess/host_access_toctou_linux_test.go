//go:build linux

package hostaccess

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/safefs"
	"golang.org/x/sys/unix"
)

// Regression tests for #1083 and #1127: tree walks over service-owned roots
// must never block on special files and must never let a mid-walk symlink
// swap redirect a chmod/chown/open outside the managed tree.

// #1083: a FIFO planted in the veil-proxy-owned state dir must not hang the
// walk, and a unix socket must not fail it — Fchownat(AT_SYMLINK_NOFOLLOW)
// re-owns both without opening them, exactly like `chown -R`.
func TestReownProxyStateDirSpecialFilesDoNotHangOrFail(t *testing.T) {
	root := t.TempDir()
	if err := unix.Mkfifo(filepath.Join(root, "planted.fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", filepath.Join(root, "admin.sock"))
	if err != nil {
		t.Fatalf("unix socket: %v", err)
	}
	defer listener.Close()
	if err := os.WriteFile(filepath.Join(root, "regular"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- reownProxyStateDir(root, os.Getuid(), os.Getgid()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("reownProxyStateDir failed on special files: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("reownProxyStateDir blocked on a FIFO (#1083)")
	}
}

// #1127: a regular file swapped for a symlink between the walk's pinned stat
// and the chmod/chown must fail closed — the operation resolves the leaf
// relative to the held parent descriptor and O_NOFOLLOW rejects it.
func TestApplyTreeOwnershipLeafSwapCannotEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	victim := filepath.Join(outside, "victim.conf")
	if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(root, "swappable")
	if err := os.WriteFile(managed, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	origStat := testHooks.statEntryAt
	defer func() { testHooks.statEntryAt = origStat }()
	testHooks.statEntryAt = func(dir *safefs.Dir, name string) (os.FileInfo, error) {
		info, err := origStat(dir, name)
		if name == "swappable" && err == nil {
			// Race: the service account swaps the just-statted file for a
			// symlink to a victim before the chmod/chown lands.
			if rmErr := os.Remove(managed); rmErr != nil {
				t.Fatalf("swap remove: %v", rmErr)
			}
			if lnErr := os.Symlink(victim, managed); lnErr != nil {
				t.Fatalf("swap symlink: %v", lnErr)
			}
		}
		return info, err
	}

	err := applyTreeOwnership(root, 0o700, 0o600, os.Getuid(), os.Getgid())
	if err == nil {
		t.Fatal("applyTreeOwnership followed a mid-walk leaf swap")
	}
	info, statErr := os.Stat(victim)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("chmod escaped the managed tree onto the victim: mode=%o", info.Mode().Perm())
	}
}

// #1127: a directory swapped for a symlink between its visit and the walk's
// descent must fail the whole pass — the descent opens the child relative to
// the pinned parent descriptor, so the symlink reports ELOOP instead of
// letting the walk recurse into an attacker-chosen tree.
func TestApplyTreeOwnershipDirSwapCannotEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	// The escape target: a directory the service account points the link at.
	if err := os.WriteFile(filepath.Join(outside, "passwd-like"), []byte("root:x:0:0"), 0o600); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "inner"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	origChmod := testHooks.chmodEntryAt
	defer func() { testHooks.chmodEntryAt = origChmod }()
	testHooks.chmodEntryAt = func(e *managedEntry, mode os.FileMode) error {
		err := origChmod(e, mode)
		if e.IsDir() && err == nil {
			// Race: between chmod(sub) and the walk's ReadDir the service
			// account replaces sub with a symlink to the outside tree.
			aside := sub + "-moved"
			if rnErr := os.Rename(sub, aside); rnErr != nil {
				t.Fatalf("swap rename: %v", rnErr)
			}
			if lnErr := os.Symlink(outside, sub); lnErr != nil {
				t.Fatalf("swap symlink: %v", lnErr)
			}
		}
		return err
	}

	err := applyTreeOwnership(root, 0o700, 0o600, os.Getuid(), os.Getgid())
	if err == nil {
		t.Fatal("applyTreeOwnership descended through a swapped directory symlink")
	}
	// The outside tree must be untouched: no chmod landed there.
	info, statErr := os.Stat(filepath.Join(outside, "passwd-like"))
	if statErr != nil {
		t.Fatal(statErr)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("chmod escaped through the swapped dir: mode=%o", info.Mode().Perm())
	}
}

// #1127 companion: a pre-planted symlinked directory inside the walked tree
// is refused outright rather than descended into.
func TestApplyTreeOwnershipRefusesSymlinkedSubdir(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	err := applyTreeOwnership(root, 0o700, 0o600, os.Getuid(), os.Getgid())
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink refusal, got %v", err)
	}
}

// #1127: the legacy-www copy must read each source relative to the pinned
// parent — a leaf swapped for a symlink mid-walk fails instead of exfiltrating
// the target into the veil-proxy-readable destination.
func TestCopyLegacyWWWTreeLeafSwapCannotExfiltrate(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "shadow")
	if err := os.WriteFile(secret, []byte("root-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(src, "page.html")
	if err := os.WriteFile(managed, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}

	origStat := testHooks.statEntryAt
	defer func() { testHooks.statEntryAt = origStat }()
	testHooks.statEntryAt = func(dir *safefs.Dir, name string) (os.FileInfo, error) {
		info, err := origStat(dir, name)
		if name == "page.html" && err == nil {
			if rmErr := os.Remove(managed); rmErr != nil {
				t.Fatalf("swap remove: %v", rmErr)
			}
			if lnErr := os.Symlink(secret, managed); lnErr != nil {
				t.Fatalf("swap symlink: %v", lnErr)
			}
		}
		return info, err
	}

	err := copyLegacyWWWTree(src, dst)
	if err == nil {
		t.Fatal("copyLegacyWWWTree copied through a swapped symlink")
	}
	if _, statErr := os.Stat(filepath.Join(dst, "page.html")); !os.IsNotExist(statErr) {
		t.Fatal("the swap target's bytes landed in the destination")
	}
}

// #1127 companion: a pre-planted symlinked subdirectory is skipped by the
// copy walk — its target's contents must never reach the destination.
func TestCopyLegacyWWWTreeSkipsSymlinkedSubdir(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("s"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(src, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := copyLegacyWWWTree(src, dst); err != nil {
		t.Fatalf("copyLegacyWWWTree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "secret")); !os.IsNotExist(err) {
		t.Fatal("symlinked subdir contents were copied into the destination")
	}
}

// #1083: a FIFO planted in a tree managed by applyTreeOwnership is refused at
// the pinned-stat check without blocking.
func TestApplyTreeOwnershipFIFORefusedFast(t *testing.T) {
	root := t.TempDir()
	if err := unix.Mkfifo(filepath.Join(root, "planted.fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- applyTreeOwnership(root, 0o700, 0o600, os.Getuid(), os.Getgid())
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "non-regular") {
			t.Fatalf("expected non-regular refusal, got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("applyTreeOwnership blocked on a FIFO (#1083)")
	}
}

// A socket planted in a managed tree is likewise refused without an ENXIO
// open attempt surfacing weirdly.
func TestApplyTreeOwnershipSocketRefusedFast(t *testing.T) {
	root := t.TempDir()
	listener, err := net.Listen("unix", filepath.Join(root, "planted.sock"))
	if err != nil {
		t.Fatalf("unix socket: %v", err)
	}
	defer listener.Close()
	done := make(chan error, 1)
	go func() {
		done <- applyTreeOwnership(root, 0o700, 0o600, os.Getuid(), os.Getgid())
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "non-regular") {
			t.Fatalf("expected non-regular refusal, got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("applyTreeOwnership blocked on a socket (#1083)")
	}
}

// #1083: the nofollow chmod/chown path hooks reject a swapped FIFO instead of
// parking on open(O_RDONLY).
func TestManagedHooksRejectFIFOWithoutBlocking(t *testing.T) {
	root := t.TempDir()
	fifo := filepath.Join(root, "planted.fifo")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	// Both hooks open O_NOFOLLOW|O_NONBLOCK then fstat-reject special files:
	// neither may block on the FIFO, and neither may apply to it.
	done := make(chan error, 1)
	go func() {
		if err := testHooks.chmod(fifo, 0o600); err == nil {
			done <- errors.New("chmod on FIFO succeeded")
			return
		}
		if err := testHooks.chown(fifo, os.Getuid(), os.Getgid()); err == nil {
			done <- errors.New("chown on FIFO succeeded")
			return
		}
		done <- nil
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("FIFO handling wrong: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a managed nofollow hook blocked on a FIFO (#1083)")
	}

	// The reown walk's Fchownat-based path must still own the FIFO inode
	// itself, without opening it — same as `chown -R`.
	pinned, err := safefs.OpenDir(root)
	if err != nil {
		t.Fatal(err)
	}
	defer pinned.Close()
	if err := pinned.ChownAt("planted.fifo", os.Getuid(), os.Getgid()); err != nil {
		t.Fatalf("ChownAt on a FIFO should succeed via fchownat: %v", err)
	}
}
