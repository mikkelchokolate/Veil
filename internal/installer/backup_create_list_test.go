package installer

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

var _backup_create_list_deps = []any{
	os.ReadFile, filepath.Join, sort.Strings, strings.Contains, testing.T{}, time.Second,
}

func TestBackupBeforeApplyBacksUpExistingFiles(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")

	// Create some files that exist
	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}

	file1 := filepath.Join(srcDir, "file1.txt")
	file2 := filepath.Join(srcDir, "file2.txt")
	if err := os.WriteFile(file1, []byte("hello file1"), 0o644); err != nil {
		t.Fatalf("write file1: %v", err)
	}
	if err := os.WriteFile(file2, []byte("hello file2"), 0o600); err != nil {
		t.Fatalf("write file2: %v", err)
	}

	// Backup
	paths := []string{file1, file2}
	backupID, err := BackupBeforeApply(paths, backupDir)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	if backupID == "" {
		t.Fatalf("expected non-empty backup ID")
	}
	// Verify ID format: YYYYMMDD_HHMMSS
	if len(backupID) != 15 {
		t.Fatalf("expected backup ID of length 15 (YYYYMMDD_HHMMSS), got %q (len=%d)", backupID, len(backupID))
	}
	if backupID[8] != '_' {
		t.Fatalf("expected underscore at position 8 in backup ID, got %q", backupID)
	}

	// Verify backup directory exists with files
	backupPath := filepath.Join(backupDir, backupID)
	entries, err := os.ReadDir(backupPath)
	if err != nil {
		t.Fatalf("read backup dir: %v", err)
	}
	// Filter out manifest.json
	fileCount := 0
	for _, e := range entries {
		if e.Name() != "manifest.json" {
			fileCount++
		}
	}
	if fileCount != 2 {
		t.Fatalf("expected 2 backed up files, got %d (entries: %v)", fileCount, entries)
	}

	// Verify file contents in backup
	backedFile1 := filepath.Join(backupPath, "file1.txt")
	backedFile2 := filepath.Join(backupPath, "file2.txt")
	assertFileContains(t, backedFile1, "hello file1")
	assertFileContains(t, backedFile2, "hello file2")
}

func TestBackupBeforeApplySkipsNonExistentFiles(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")

	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}

	file1 := filepath.Join(srcDir, "exists.txt")
	if err := os.WriteFile(file1, []byte("content"), 0o644); err != nil {
		t.Fatalf("write file1: %v", err)
	}

	// Include a non-existent file
	file2 := filepath.Join(srcDir, "nonexistent.txt")
	paths := []string{file1, file2}

	backupID, err := BackupBeforeApply(paths, backupDir)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	if backupID == "" {
		t.Fatalf("expected non-empty backup ID")
	}

	backupPath := filepath.Join(backupDir, backupID)
	entries, err := os.ReadDir(backupPath)
	if err != nil {
		t.Fatalf("read backup dir: %v", err)
	}
	fileCount := 0
	for _, e := range entries {
		if e.Name() != "manifest.json" {
			fileCount++
		}
	}
	if fileCount != 1 {
		t.Fatalf("expected 1 backed up file (skipped nonexistent), got %d (entries: %v)", fileCount, entries)
	}
}

func TestBackupBeforeApplyEmptyPathsReturnsNoOp(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")

	backupID, err := BackupBeforeApply([]string{}, backupDir)
	if err != nil {
		t.Fatalf("backup empty: %v", err)
	}
	if backupID == "" {
		t.Fatalf("expected non-empty backup ID even for empty paths")
	}

	// Backup dir should be empty (only manifest.json)
	backupPath := filepath.Join(backupDir, backupID)
	entries, err := os.ReadDir(backupPath)
	if err != nil {
		t.Fatalf("read backup dir: %v", err)
	}
	fileCount := 0
	for _, e := range entries {
		if e.Name() != "manifest.json" {
			fileCount++
		}
	}
	if fileCount != 0 {
		t.Fatalf("expected 0 backed up files, got %d (entries: %v)", fileCount, entries)
	}
}

func TestListBackupsReturnsSortedIDs(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")

	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	file1 := filepath.Join(srcDir, "file.txt")
	if err := os.WriteFile(file1, []byte("c"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Create multiple backups
	id1, err := BackupBeforeApply([]string{file1}, backupDir)
	if err != nil {
		t.Fatalf("backup 1: %v", err)
	}
	// Small pause to ensure different timestamps (backup ID uses second resolution)
	time.Sleep(1100 * time.Millisecond)
	id2, err := BackupBeforeApply([]string{file1}, backupDir)
	if err != nil {
		t.Fatalf("backup 2: %v", err)
	}

	ids, err := ListBackups(backupDir)
	if err != nil {
		t.Fatalf("list backups: %v", err)
	}
	if len(ids) < 2 {
		t.Fatalf("expected at least 2 backups, got %d: %v", len(ids), ids)
	}

	// Verify sorted order
	sorted := make([]string, len(ids))
	copy(sorted, ids)
	sort.Slice(sorted, func(i, j int) bool {
		return strings.Compare(sorted[i], sorted[j]) < 0
	})
	for i := range ids {
		if ids[i] != sorted[i] {
			t.Fatalf("expected sorted IDs, got %v, sorted %v", ids, sorted)
		}
	}

	// Verify our IDs are in the list
	found1, found2 := false, false
	for _, id := range ids {
		if id == id1 {
			found1 = true
		}
		if id == id2 {
			found2 = true
		}
	}
	if !found1 || !found2 {
		t.Fatalf("expected to find backup IDs %s and %s in list %v", id1, id2, ids)
	}
}

func TestListBackupsEmptyDirReturnsEmptyList(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")

	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backup dir: %v", err)
	}

	ids, err := ListBackups(backupDir)
	if err != nil {
		t.Fatalf("list backups: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected empty list, got %v", ids)
	}
}

func TestListBackupsNonExistentDirReturnsEmptyList(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "nonexistent")

	ids, err := ListBackups(backupDir)
	if err != nil {
		t.Fatalf("list backups: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected empty list for non-existent dir, got %v", ids)
	}
}
