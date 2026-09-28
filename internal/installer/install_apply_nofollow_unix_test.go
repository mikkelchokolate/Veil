//go:build unix

package installer

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// #1129: the install/repair ownership hooks must default to no-follow
// operations so a managed file swapped for a symlink between atomic
// write+rename and the ownership pass cannot redirect chmod/chown onto an
// arbitrary root-owned victim.

func TestChmodPathDefaultRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "swapped")
	if err := os.Symlink(victim, link); err != nil {
		t.Fatal(err)
	}
	if err := chmodPath(link, 0o777); err == nil {
		t.Fatal("chmodPath followed a symlink")
	}
	info, err := os.Stat(victim)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("chmod escaped through the symlink: mode=%o", info.Mode().Perm())
	}
}

func TestChownPathDefaultDoesNotFollowSymlink(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "swapped")
	if err := os.Symlink(victim, link); err != nil {
		t.Fatal(err)
	}
	// ChownNoFollow opens with O_NOFOLLOW first, so a swapped symlink fails
	// ELOOP outright rather than chowning the target.
	if err := chownPath(link, os.Getuid(), os.Getgid()); err == nil {
		t.Fatal("chownPath followed a symlink")
	}
	var victimStat syscall.Stat_t
	if err := syscall.Stat(victim, &victimStat); err != nil {
		t.Fatal(err)
	}
	if int(victimStat.Uid) != os.Getuid() {
		t.Fatal("chown escaped through the symlink onto the victim")
	}
}
