package backup

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

var _backup_restore_deps = []any{
	os.ReadFile, filepath.Join, sort.Strings, strings.Contains, testing.T{}, time.Second,
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

func TestRestoreFromBackupCreatesSafetyBackup(t *testing.T) {
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

	// Create initial backup (the one we'll restore from)
	backupID, err := BackupBeforeApply([]string{file1, file2}, backupDir)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}

	// Modify files to simulate current live state
	modified1 := "listen: :8443\npassword: newpass\n"
	modified2 := "[Unit]\nDescription=Modified\n"
	if err := os.WriteFile(file1, []byte(modified1), 0o600); err != nil {
		t.Fatalf("modify file1: %v", err)
	}
	if err := os.WriteFile(file2, []byte(modified2), 0o644); err != nil {
		t.Fatalf("modify file2: %v", err)
	}

	// Count backups before restore
	beforeIDs, err := ListBackups(backupDir)
	if err != nil {
		t.Fatalf("list backups before: %v", err)
	}
	beforeCount := len(beforeIDs)

	// Restore from the original backup
	restored, err := RestoreFromBackup(backupDir, backupID)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if len(restored) != 2 {
		t.Fatalf("expected 2 restored, got %d", len(restored))
	}

	// Verify files are restored to original content
	assertFileContains(t, file1, original1)
	assertFileContains(t, file2, original2)

	// Verify a safety backup was created (one more backup than before)
	afterIDs, err := ListBackups(backupDir)
	if err != nil {
		t.Fatalf("list backups after: %v", err)
	}
	if len(afterIDs) != beforeCount+1 {
		t.Fatalf("expected one new safety backup, before=%d after=%d ids=%v", beforeCount, len(afterIDs), afterIDs)
	}

	// Find the safety backup (the one that is not backupID)
	var safetyID string
	for _, id := range afterIDs {
		if id != backupID {
			safetyID = id
			break
		}
	}
	if safetyID == "" {
		t.Fatal("could not find safety backup ID")
	}

	// Verify safety backup contains the modified (pre-restore) files
	safetyBackupPath := filepath.Join(backupDir, safetyID)
	manifestData, err := os.ReadFile(filepath.Join(safetyBackupPath, "manifest.json"))
	if err != nil {
		t.Fatalf("read safety manifest: %v", err)
	}
	if !strings.Contains(string(manifestData), file1) {
		t.Fatalf("safety backup manifest should contain %s, got: %s", file1, string(manifestData))
	}
	if !strings.Contains(string(manifestData), file2) {
		t.Fatalf("safety backup manifest should contain %s, got: %s", file2, string(manifestData))
	}

	// Verify safety backup contains the modified content (not the original)
	// The safety backup should have the files as they were BEFORE restore
	safetyFiles, err := os.ReadDir(safetyBackupPath)
	if err != nil {
		t.Fatalf("read safety backup dir: %v", err)
	}
	for _, f := range safetyFiles {
		if f.Name() == "manifest.json" {
			continue
		}
		content, err := os.ReadFile(filepath.Join(safetyBackupPath, f.Name()))
		if err != nil {
			t.Fatalf("read safety file %s: %v", f.Name(), err)
		}
		if strings.Contains(string(content), "newpass") || strings.Contains(string(content), "Modified") {
			// Found modified content in safety backup - this is correct
			return
		}
	}
	t.Fatal("safety backup should contain the modified pre-restore content")
}
