//go:build !windows

package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// Rewrites must not expose a root-owned window: readers that rely on group
// ownership (veil-proxy units reading tls certs) would crash on permission
// denied if the file briefly reverted to root:root between WriteFile and the
// installer's later chown pass. Ownership is applied to the staged temp file
// before rename so the replacement never regresses.
func TestWritePreservesExistingOwner(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("ownership preservation only applies to root writes")
	}
	path := filepath.Join(t.TempDir(), "tls.crt")
	if err := os.WriteFile(path, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(path, 1234, 1235); err != nil {
		t.Skipf("cannot chown fixture: %v", err)
	}
	if err := Write(path, []byte("new"), 0o640, 0o750); err != nil {
		t.Fatalf("Write: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	st := info.Sys().(*syscall.Stat_t)
	if st.Uid != 1234 || st.Gid != 1235 {
		t.Fatalf("owner = %d:%d, want 1234:1235", st.Uid, st.Gid)
	}
	if body, _ := os.ReadFile(path); string(body) != "new" {
		t.Fatalf("body = %q", body)
	}
}

// New files keep the writer's ownership — there is no prior contract to
// preserve, and the caller's post-write chown pass sets the final owner.
func TestWriteNewFileDoesNotPreserve(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-only assertion")
	}
	path := filepath.Join(t.TempDir(), "fresh.crt")
	if err := Write(path, []byte("body"), 0o640, 0o750); err != nil {
		t.Fatalf("Write: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st := info.Sys().(*syscall.Stat_t); st.Uid != 0 {
		t.Fatalf("new file uid = %d, want 0", st.Uid)
	}
}

// A chown failure aborts the write before rename — a half-owned replacement
// would regress readers mid-flight just like the unpatched window.
func TestWriteFailsWhenOwnerPreservationFails(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("ownership preservation only applies to root writes")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "tls.crt")
	if err := os.WriteFile(path, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	orig := chownFile
	chownFile = func(string, int, int) error { return errors.New("chown denied") }
	defer func() { chownFile = orig }()
	if err := Write(path, []byte("new"), 0o640, 0o750); err == nil {
		t.Fatal("Write must fail when ownership preservation fails")
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "old" {
		t.Fatalf("target must be untouched after aborted write: %q %v", body, err)
	}
}
