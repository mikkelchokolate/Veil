package installer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBinaryAtomicWriterCreatesParentAndWritesMode(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "bin", "veil")
	if err := NewBinaryAtomicWriter().Write(dest, []byte("binary"), 0o755); err != nil {
		t.Fatalf("Write: %v", err)
	}
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != "binary" {
		t.Fatalf("body = %q", string(body))
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}
