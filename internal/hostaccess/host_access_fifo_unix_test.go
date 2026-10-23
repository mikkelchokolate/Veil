//go:build unix

package hostaccess

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// Split out of TestApplyTreeOwnership: syscall.Mkfifo does not exist on
// Windows, so an untagged subtest kept the package test binary from
// compiling there.
func TestApplyTreeOwnershipRejectsNonRegularPath(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "tree")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "fifo"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := applyTreeOwnership(dir, 0o700, 0o600, os.Getuid(), os.Getgid(), true)
	if err == nil || !strings.Contains(err.Error(), "refuse to migrate non-regular path") {
		t.Fatalf("expected non-regular error, got: %v", err)
	}
}
