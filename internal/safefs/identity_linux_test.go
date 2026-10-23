package safefs

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// The helper persists this tag as InstalledPathInode in update evidence and
// the panel compares it verbatim, so the field order is a compatibility
// contract, not just "four colon-separated numbers". This pin used to live in
// internal/apply's update-evidence test as a hand-built Stat_t.Ctim string,
// which kept that package's tests from compiling off Linux.
func TestFileIdentityLinuxFieldOrderIsDevInoCtime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "binary")
	if err := os.WriteFile(path, []byte("veil"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	stat := info.Sys().(*syscall.Stat_t)
	want := fmt.Sprintf("%d:%d:%d:%d", stat.Dev, stat.Ino, stat.Ctim.Sec, stat.Ctim.Nsec)
	if got, ok := FileIdentity(info); !ok || got != want {
		t.Fatalf("FileIdentity = %q (ok=%v), want %q", got, ok, want)
	}
}
