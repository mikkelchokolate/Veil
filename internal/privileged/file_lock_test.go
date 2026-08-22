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
