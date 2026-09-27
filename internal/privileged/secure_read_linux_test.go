//go:build linux

package privileged

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// #1083: readBoundedRegularFile opens with O_NOFOLLOW|O_NONBLOCK and fstas —
// a FIFO planted where a regular file is expected must error out instead of
// parking the privileged executor on open() until a writer shows up.
func TestReadBoundedRegularFileFIFODoesNotBlock(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "planted.fifo")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := readBoundedRegularFile(fifo, 1024)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("readBoundedRegularFile served a FIFO")
		}
		if !strings.Contains(err.Error(), "regular file") {
			t.Fatalf("unexpected error text: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("readBoundedRegularFile blocked on a FIFO (#1083)")
	}
}

// #1083 companion: a regular file swapped for a FIFO between the caller's
// intent and the open still refuses — O_NOFOLLOW pins the leaf, O_NONBLOCK
// keeps the open cheap, fstat rejects the wrong kind.
func TestReadBoundedRegularFileRegularToFIFOSwap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "target")
	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := readBoundedRegularFile(path, 1024)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("readBoundedRegularFile served a swapped-in FIFO")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("readBoundedRegularFile blocked after the swap (#1083)")
	}
}

// The general no-follow opener must still serve directories and refuse
// symlinked leaves for the ownership hooks.
func TestOpenNoFollowRejectsSymlinkLeaf(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := openNoFollow(link); err == nil {
		t.Fatal("openNoFollow followed a symlink")
	} else if !errors.Is(err, unix.ELOOP) {
		t.Fatalf("openNoFollow error = %v, want ELOOP", err)
	}
}

func TestChownNoFollowDoesNotTouchSymlinkTarget(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "swapped")
	if err := os.Symlink(victim, link); err != nil {
		t.Fatal(err)
	}
	// The O_NOFOLLOW open rejects the swapped leaf outright (ELOOP) — the
	// chown can never reach through to the target.
	if err := chownNoFollow(link, os.Getuid(), os.Getgid()); err == nil {
		t.Fatal("chownNoFollow followed a symlink")
	}
	info, err := os.Stat(victim)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("victim mutated through the symlink: mode=%o", info.Mode().Perm())
	}
}
