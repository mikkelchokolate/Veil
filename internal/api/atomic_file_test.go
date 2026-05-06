package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAtomicFileCreatesParentsAndSetsMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "file.txt")
	if err := writeAtomicFile(path, []byte("body"), 0o640); err != nil {
		t.Fatalf("writeAtomicFile: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(body) != "body" {
		t.Fatalf("body = %q", body)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}
