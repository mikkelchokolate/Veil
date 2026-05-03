package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/veil-panel/veil/internal/installer"
)

func TestRollbackListShowsBackupIDs(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")

	// Create a backup
	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	file1 := filepath.Join(srcDir, "file.txt")
	if err := os.WriteFile(file1, []byte("content"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	backupID, err := installer.BackupBeforeApply([]string{file1}, backupDir)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"rollback", "list", "--backup-dir", backupDir})

	err = cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out.String())
	}
	if !strings.Contains(out.String(), backupID) {
		t.Fatalf("expected output to contain backup ID %q, got:\n%s", backupID, out.String())
	}
}

func TestRollbackListEmptyDirShowsNothing(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backup dir: %v", err)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"rollback", "list", "--backup-dir", backupDir})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out.String())
	}
	// Should not crash and should print something (even if empty)
}

func TestRollbackRestoreBringsFilesBack(t *testing.T) {
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
	backupID, err := installer.BackupBeforeApply([]string{file1, file2}, backupDir)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}

	// Modify files
	if err := os.WriteFile(file1, []byte("modified 1\n"), 0o600); err != nil {
		t.Fatalf("modify file1: %v", err)
	}
	if err := os.WriteFile(file2, []byte("modified 2\n"), 0o644); err != nil {
		t.Fatalf("modify file2: %v", err)
	}

	// Restore via CLI
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"rollback", "restore", backupID, "--backup-dir", backupDir, "--yes"})

	err = cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out.String())
	}

	// Verify files are restored
	body1, err := os.ReadFile(file1)
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

	// Output should list restored file paths
	if !strings.Contains(out.String(), file1) {
		t.Fatalf("expected output to contain restored path %q, got:\n%s", file1, out.String())
	}
	if !strings.Contains(out.String(), file2) {
		t.Fatalf("expected output to contain restored path %q, got:\n%s", file2, out.String())
	}
}

func TestRollbackRestoreWithoutYesFails(t *testing.T) {
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
	backupID, err := installer.BackupBeforeApply([]string{file1}, backupDir)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"rollback", "restore", backupID, "--backup-dir", backupDir})

	err = cmd.Execute()
	if err == nil {
		t.Fatalf("expected error for restore without --yes, got nil\noutput: %s", out.String())
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("expected error to mention --yes, got: %v", err)
	}
}

func TestRollbackRestoreNonExistentBackupIDFails(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backup dir: %v", err)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"rollback", "restore", "20240101_120000", "--backup-dir", backupDir, "--yes"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error for non-existent backup ID, got nil\noutput: %s", out.String())
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("expected error to contain 'does not exist', got: %v", err)
	}
}

func TestRollbackCleanupRemovesBackup(t *testing.T) {
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
	backupID, err := installer.BackupBeforeApply([]string{file1}, backupDir)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}

	// Verify backup exists
	backupPath := filepath.Join(backupDir, backupID)
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("backup should exist: %v", err)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"rollback", "cleanup", backupID, "--backup-dir", backupDir, "--yes"})

	err = cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out.String())
	}

	// Verify backup is gone
	if _, err := os.Stat(backupPath); !os.IsNotExist(err) {
		t.Fatalf("backup should be removed after cleanup")
	}
}

func TestRollbackCleanupWithoutYesFails(t *testing.T) {
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
	backupID, err := installer.BackupBeforeApply([]string{file1}, backupDir)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"rollback", "cleanup", backupID, "--backup-dir", backupDir})

	err = cmd.Execute()
	if err == nil {
		t.Fatalf("expected error for cleanup without --yes, got nil\noutput: %s", out.String())
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("expected error to mention --yes, got: %v", err)
	}
}

func TestRollbackWithoutBackupDirFails(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"list", []string{"rollback", "list"}},
		{"restore", []string{"rollback", "restore", "20240101_120000", "--yes"}},
		{"cleanup", []string{"rollback", "cleanup", "20240101_120000", "--yes"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewRootCommand("test")
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs(tt.args)

			err := cmd.Execute()
			if err == nil {
				t.Fatalf("expected error for %s without --backup-dir, got nil\noutput: %s", tt.name, out.String())
			}
			if !strings.Contains(err.Error(), "--backup-dir") {
				t.Fatalf("expected error to mention --backup-dir for %s, got: %v", tt.name, err)
			}
		})
	}
}

func readAuditLog(t *testing.T, path string) []map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var events []map[string]interface{}
	for _, line := range lines {
		if line == "" {
			continue
		}
		var ev map[string]interface{}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("unmarshal audit line: %v", err)
		}
		events = append(events, ev)
	}
	return events
}

func TestRollbackRestoreWithAuditLog(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	auditPath := filepath.Join(dir, "audit.jsonl")

	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	file1 := filepath.Join(srcDir, "config.yaml")
	file2 := filepath.Join(srcDir, "service.conf")
	if err := os.WriteFile(file1, []byte("original 1\n"), 0o600); err != nil {
		t.Fatalf("write file1: %v", err)
	}
	if err := os.WriteFile(file2, []byte("original 2\n"), 0o644); err != nil {
		t.Fatalf("write file2: %v", err)
	}

	backupID, err := installer.BackupBeforeApply([]string{file1, file2}, backupDir)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}

	// Modify files
	if err := os.WriteFile(file1, []byte("modified\n"), 0o600); err != nil {
		t.Fatalf("modify file1: %v", err)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"rollback", "restore", backupID, "--backup-dir", backupDir, "--yes", "--audit-log", auditPath})

	err = cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out.String())
	}

	// Verify audit log exists
	events := readAuditLog(t, auditPath)
	if len(events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(events))
	}
	ev := events[0]
	if ev["action"] != "rollback.restore" {
		t.Fatalf("expected action 'rollback.restore', got %v", ev["action"])
	}
	if ev["backupID"] != backupID {
		t.Fatalf("expected backupID %q, got %v", backupID, ev["backupID"])
	}
	if ev["success"] != true {
		t.Fatalf("expected success=true, got %v", ev["success"])
	}
	if ev["timestamp"] == nil || ev["timestamp"] == "" {
		t.Fatalf("expected non-empty timestamp")
	}
	rf, ok := ev["restoredFiles"].([]interface{})
	if !ok {
		t.Fatalf("expected restoredFiles array, got %T", ev["restoredFiles"])
	}
	if len(rf) != 2 {
		t.Fatalf("expected 2 restoredFiles, got %d: %v", len(rf), rf)
	}
}

func TestRollbackRestoreFailureWithAuditLog(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	auditPath := filepath.Join(dir, "audit.jsonl")

	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backup dir: %v", err)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"rollback", "restore", "nonexistent_id", "--backup-dir", backupDir, "--yes", "--audit-log", auditPath})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error, got nil\noutput: %s", out.String())
	}

	// Verify audit log has failure event
	events := readAuditLog(t, auditPath)
	if len(events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(events))
	}
	ev := events[0]
	if ev["action"] != "rollback.restore" {
		t.Fatalf("expected action 'rollback.restore', got %v", ev["action"])
	}
	if ev["success"] != false {
		t.Fatalf("expected success=false, got %v", ev["success"])
	}
	if ev["error"] == nil || ev["error"] == "" {
		t.Fatalf("expected non-empty error field, got %v", ev["error"])
	}
}

func TestRollbackCleanupWithAuditLog(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	auditPath := filepath.Join(dir, "audit.jsonl")

	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	file1 := filepath.Join(srcDir, "file.txt")
	if err := os.WriteFile(file1, []byte("content"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	backupID, err := installer.BackupBeforeApply([]string{file1}, backupDir)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"rollback", "cleanup", backupID, "--backup-dir", backupDir, "--yes", "--audit-log", auditPath})

	err = cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out.String())
	}

	// Verify audit log
	events := readAuditLog(t, auditPath)
	if len(events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(events))
	}
	ev := events[0]
	if ev["action"] != "rollback.cleanup" {
		t.Fatalf("expected action 'rollback.cleanup', got %v", ev["action"])
	}
	if ev["backupID"] != backupID {
		t.Fatalf("expected backupID %q, got %v", backupID, ev["backupID"])
	}
	if ev["success"] != true {
		t.Fatalf("expected success=true, got %v", ev["success"])
	}
}

func TestRollbackRestoreNoAuditFlagBackwardCompatible(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")

	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	file1 := filepath.Join(srcDir, "file.txt")
	if err := os.WriteFile(file1, []byte("original"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	backupID, err := installer.BackupBeforeApply([]string{file1}, backupDir)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}

	// Modify
	if err := os.WriteFile(file1, []byte("modified"), 0o644); err != nil {
		t.Fatalf("modify file: %v", err)
	}

	// Restore without --audit-log
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"rollback", "restore", backupID, "--backup-dir", backupDir, "--yes"})

	err = cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error without --audit-log: %v\noutput: %s", err, out.String())
	}

	// Verify no audit file was created in default locations
	body, err := os.ReadFile(file1)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(body) != "original" {
		t.Fatalf("file not restored: got %q", string(body))
	}
}
