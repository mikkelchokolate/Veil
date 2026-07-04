package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultRetentionPolicy(t *testing.T) {
	p := DefaultRetentionPolicy()
	if p.Daily != 7 || p.Weekly != 4 || p.Monthly != 12 {
		t.Fatalf("unexpected default policy: %+v", p)
	}
}

func TestListArchivesReadDirError(t *testing.T) {
	dir := t.TempDir()
	// Use a file path as the archive dir; ReadDir returns ENOTDIR.
	path := filepath.Join(dir, "notadir")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ListArchives(path); err == nil {
		t.Fatal("expected error")
	}
}

func TestPruneArchivesNegativeRetention(t *testing.T) {
	if _, err := PruneArchives(t.TempDir(), RetentionPolicy{Daily: -1}, false); err == nil {
		t.Fatal("expected error for negative retention")
	}
}
