package installer

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

var _backup_cleanup_deps = []any{
	os.ReadFile, filepath.Join, sort.Strings, strings.Contains, testing.T{}, time.Second,
}

func TestCleanupBackupRemovesBackupDirectory(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")

	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}

	file1 := filepath.Join(srcDir, "file.txt")
	if err := os.WriteFile(file1, []byte("content"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	backupID, err := BackupBeforeApply([]string{file1}, backupDir)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}

	// Verify backup exists
	backupPath := filepath.Join(backupDir, backupID)
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("backup should exist: %v", err)
	}

	// Cleanup
	if err := CleanupBackup(backupDir, backupID); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	// Verify backup is gone
	if _, err := os.Stat(backupPath); !os.IsNotExist(err) {
		t.Fatalf("backup should be removed after cleanup")
	}
}

func TestCleanupNonExistentBackupReturnsError(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")

	err := CleanupBackup(backupDir, "20240101_120000")
	if err == nil {
		t.Fatalf("expected error for cleanup of non-existent backup")
	}
}
