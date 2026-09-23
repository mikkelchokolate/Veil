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
// before rename so the replacement never regresses. The geteuid/chownFile
// hooks let the behavior run asserted under the unprivileged CI user.
func TestWritePreservesExistingOwner(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tls.crt")
	if err := os.WriteFile(path, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	targetInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	want := targetInfo.Sys().(*syscall.Stat_t)

	var gotPath string
	var gotUID, gotGID int
	calls := 0
	origChown, origEuid := chownFile, geteuid
	chownFile = func(p string, uid, gid int) error {
		calls++
		gotPath, gotUID, gotGID = p, uid, gid
		return nil
	}
	geteuid = func() int { return 0 }
	defer func() { chownFile, geteuid = origChown, origEuid }()

	if err := Write(path, []byte("new"), 0o640, 0o750); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if calls != 1 {
		t.Fatalf("chown calls = %d, want 1 (staged temp file before rename)", calls)
	}
	if gotUID != int(want.Uid) || gotGID != int(want.Gid) {
		t.Fatalf("preserved owner = %d:%d, want replaced file's %d:%d", gotUID, gotGID, want.Uid, want.Gid)
	}
	if filepath.Dir(gotPath) != dir || filepath.Base(gotPath) == "tls.crt" {
		t.Fatalf("chown must target the staged temp file in the same dir, got %q", gotPath)
	}
	if body, _ := os.ReadFile(path); string(body) != "new" {
		t.Fatalf("body = %q", body)
	}
}

// New files keep the writer's ownership — there is no prior contract to
// preserve, and the caller's post-write chown pass sets the final owner.
func TestWriteNewFileDoesNotPreserve(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.crt")

	calls := 0
	origChown, origEuid := chownFile, geteuid
	chownFile = func(string, int, int) error { calls++; return nil }
	geteuid = func() int { return 0 }
	defer func() { chownFile, geteuid = origChown, origEuid }()

	if err := Write(path, []byte("body"), 0o640, 0o750); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if calls != 0 {
		t.Fatalf("chown calls = %d, want 0 for a new file", calls)
	}
}

// Non-root writers skip preservation entirely — they could not chown anyway,
// and their temp file already carries the only uid they can produce.
func TestWriteSkipsPreserveForNonRoot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tls.crt")
	if err := os.WriteFile(path, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	calls := 0
	origChown, origEuid := chownFile, geteuid
	chownFile = func(string, int, int) error { calls++; return nil }
	geteuid = func() int { return 1000 }
	defer func() { chownFile, geteuid = origChown, origEuid }()

	if err := Write(path, []byte("new"), 0o640, 0o750); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if calls != 0 {
		t.Fatalf("chown calls = %d, want 0 for non-root writer", calls)
	}
}

// A chown failure aborts the write before rename — a half-owned replacement
// would regress readers mid-flight just like the unpatched window.
func TestWriteFailsWhenOwnerPreservationFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tls.crt")
	if err := os.WriteFile(path, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	origChown, origEuid := chownFile, geteuid
	chownFile = func(string, int, int) error { return errors.New("chown denied") }
	geteuid = func() int { return 0 }
	defer func() { chownFile, geteuid = origChown, origEuid }()

	if err := Write(path, []byte("new"), 0o640, 0o750); err == nil {
		t.Fatal("Write must fail when ownership preservation fails")
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "old" {
		t.Fatalf("target must be untouched after aborted write: %q %v", body, err)
	}
}
