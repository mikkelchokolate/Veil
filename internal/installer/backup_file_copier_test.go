package installer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupFileCopierCopiesContentsAndMode(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("secret"), 0o640); err != nil {
		t.Fatalf("write src: %v", err)
	}
	if err := NewBackupFileCopier().Copy(src, dst, 0o600); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	body, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(body) != "secret" {
		t.Fatalf("body = %q", string(body))
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat dst: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}
