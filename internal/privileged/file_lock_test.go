package privileged

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestReleaseLockedFileReportsCloseFailure(t *testing.T) {
	file, err := os.OpenFile(filepath.Join(t.TempDir(), "lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := releaseLockedFile(file); err != nil {
		t.Fatalf("release lock: %v", err)
	}
	if err := releaseLockedFile(file); err == nil {
		t.Fatal("expected releasing a closed lock file to fail")
	}
}

// lockFileExclusive must take a real exclusive flock: a second descriptor on
// the same lock file cannot acquire it until the holder releases (#1243 moved
// the flock call behind a per-OS helper; this pins that it still locks).
func TestLockFileExclusiveExcludesSecondDescriptor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	holder, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := lockFileExclusive(holder); err != nil {
		_ = holder.Close()
		t.Fatalf("lock: %v", err)
	}
	contender, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		_ = releaseLockedFile(holder)
		t.Fatal(err)
	}
	defer contender.Close()
	if err := unix.Flock(int(contender.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != unix.EWOULDBLOCK {
		_ = releaseLockedFile(holder)
		t.Fatalf("contender flock while held = %v, want EWOULDBLOCK", err)
	}
	if err := releaseLockedFile(holder); err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := unix.Flock(int(contender.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatalf("contender flock after release = %v, want success", err)
	}
}
