package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUpdateBinaryFilesReplacesAndRollsBackBinary(t *testing.T) {
	dir := t.TempDir()
	currentPath := filepath.Join(dir, "veil")
	backupPath := currentPath + ".backup"
	files := NewUpdateBinaryFiles()

	if err := os.WriteFile(currentPath, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := files.Copy(currentPath, backupPath); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if err := files.ReplaceAtomic(currentPath, []byte("new")); err != nil {
		t.Fatalf("ReplaceAtomic: %v", err)
	}
	if err := files.Rollback(backupPath, currentPath); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	body, err := os.ReadFile(currentPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "old" {
		t.Fatalf("body = %q", string(body))
	}
	if _, err := os.Stat(backupPath); !os.IsNotExist(err) {
		t.Fatalf("backup should be removed after rollback")
	}
}
