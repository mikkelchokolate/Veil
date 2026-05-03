package installer

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

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

func TestRestoreFromBackupBringsFilesBack(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")

	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}

	file1 := filepath.Join(srcDir, "file1.txt")
	file2 := filepath.Join(srcDir, "file2.txt")
	if err := os.WriteFile(file1, []byte("original content 1"), 0o644); err != nil {
		t.Fatalf("write file1: %v", err)
	}
	if err := os.WriteFile(file2, []byte("original content 2"), 0o600); err != nil {
		t.Fatalf("write file2: %v", err)
	}

	// Backup
	paths := []string{file1, file2}
	backupID, err := BackupBeforeApply(paths, backupDir)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}

	// Overwrite originals
	if err := os.WriteFile(file1, []byte("modified 1"), 0o755); err != nil {
		t.Fatalf("overwrite file1: %v", err)
	}
	if err := os.WriteFile(file2, []byte("modified 2"), 0o755); err != nil {
		t.Fatalf("overwrite file2: %v", err)
	}

	// Restore
	restored, err := RestoreFromBackup(backupDir, backupID)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if len(restored) != 2 {
		t.Fatalf("expected 2 restored files, got %d: %v", len(restored), restored)
	}

	// Verify original contents are back
	assertFileContains(t, file1, "original content 1")
	assertFileContains(t, file2, "original content 2")
}

func TestRestoreFromNonExistentBackupReturnsError(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")

	_, err := RestoreFromBackup(backupDir, "20240101_120000")
	if err == nil {
		t.Fatalf("expected error for non-existent backup")
	}
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

func TestBackupThenModifyThenRestoreVerifiesContent(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")

	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}

	file1 := filepath.Join(srcDir, "config.yaml")
	file2 := filepath.Join(srcDir, "service.conf")
	original1 := "listen: :443\npassword: secret\n"
	original2 := "[Unit]\nDescription=Test\n"
	if err := os.WriteFile(file1, []byte(original1), 0o600); err != nil {
		t.Fatalf("write file1: %v", err)
	}
	if err := os.WriteFile(file2, []byte(original2), 0o644); err != nil {
		t.Fatalf("write file2: %v", err)
	}

	// Backup
	backupID, err := BackupBeforeApply([]string{file1, file2}, backupDir)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}

	// Simulate apply: overwrite with new content
	if err := os.WriteFile(file1, []byte("listen: :8443\npassword: newpass\n"), 0o600); err != nil {
		t.Fatalf("overwrite file1: %v", err)
	}
	if err := os.WriteFile(file2, []byte("[Unit]\nDescription=Modified\n"), 0o644); err != nil {
		t.Fatalf("overwrite file2: %v", err)
	}

	// Verify content changed
	body1, _ := os.ReadFile(file1)
	if string(body1) == original1 {
		t.Fatalf("file1 should have been modified")
	}

	// Restore
	restored, err := RestoreFromBackup(backupDir, backupID)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if len(restored) != 2 {
		t.Fatalf("expected 2 restored, got %d", len(restored))
	}

	// Verify original content is back exactly
	body1, err = os.ReadFile(file1)
	if err != nil {
		t.Fatalf("read file1: %v", err)
	}
	if string(body1) != original1 {
		t.Fatalf("file1 content mismatch:\ngot:  %q\nwant: %q", string(body1), original1)
	}

	body2, err := os.ReadFile(file2)
	if err != nil {
		t.Fatalf("read file2: %v", err)
	}
	if string(body2) != original2 {
		t.Fatalf("file2 content mismatch:\ngot:  %q\nwant: %q", string(body2), original2)
	}
}

func TestBackupPreservesFileMode(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")

	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}

	file1 := filepath.Join(srcDir, "executable.sh")
	if err := os.WriteFile(file1, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}

	backupID, err := BackupBeforeApply([]string{file1}, backupDir)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}

	// Overwrite
	if err := os.WriteFile(file1, []byte("modified"), 0o644); err != nil {
		t.Fatalf("overwrite: %v", err)
	}

	// Restore
	_, err = RestoreFromBackup(backupDir, backupID)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}

	// Check file mode is restored
	info, err := os.Stat(file1)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("expected mode 0755, got %o", info.Mode().Perm())
	}
}

func TestRestoreRecreatesMissingParentDirectories(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")

	srcDir := filepath.Join(dir, "src", "sub", "deep")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}

	file1 := filepath.Join(srcDir, "nested.txt")
	if err := os.WriteFile(file1, []byte("nested content"), 0o644); err != nil {
		t.Fatalf("write nested: %v", err)
	}

	backupID, err := BackupBeforeApply([]string{file1}, backupDir)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}

	// Remove the entire source tree
	if err := os.RemoveAll(filepath.Join(dir, "src")); err != nil {
		t.Fatalf("remove src tree: %v", err)
	}

	// Restore - should recreate parent directories
	_, err = RestoreFromBackup(backupDir, backupID)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}

	// Verify file is back with correct content
	assertFileContains(t, file1, "nested content")
}
