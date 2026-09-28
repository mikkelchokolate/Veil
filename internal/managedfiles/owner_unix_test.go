//go:build unix

package managedfiles

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// #1129: chownFile must operate on an O_NOFOLLOW-pinned descriptor so a leaf
// swapped for a symlink between atomicfile.Write's rename and the chown
// re-owns only the link itself, never the target.

func TestChownFileDoesNotFollowSymlink(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "swapped")
	if err := os.Symlink(victim, link); err != nil {
		t.Fatal(err)
	}

	// The no-follow open rejects a swapped symlink with ELOOP instead of
	// chowning through to the target.
	if err := chownFile(link, os.Getuid(), os.Getgid()); err == nil {
		t.Fatal("chownFile followed a symlink")
	}
	var victimStat syscall.Stat_t
	if err := syscall.Stat(victim, &victimStat); err != nil {
		t.Fatal(err)
	}
	if int(victimStat.Uid) != os.Getuid() {
		t.Fatal("chown escaped through the symlink onto the victim")
	}
}
