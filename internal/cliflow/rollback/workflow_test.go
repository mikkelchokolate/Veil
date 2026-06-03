package rollback

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/backup"
)

func TestWorkflowListsAndRestoresBackup(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	managed := filepath.Join(dir, "veil.env")
	if err := os.WriteFile(managed, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	backupID, err := backup.NewLifecycle(backupDir).BackupExisting([]string{managed})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managed, []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	workflow := NewWorkflow(Options{BackupDir: backupDir, Yes: true}, &out)
	if err := workflow.List(); err != nil {
		t.Fatalf("List: %v", err)
	}
	if !strings.Contains(out.String(), backupID) {
		t.Fatalf("list output missing backup ID: %s", out.String())
	}
	out.Reset()
	if err := workflow.Restore(backupID); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	body, err := os.ReadFile(managed)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "before" {
		t.Fatalf("restored body = %q", string(body))
	}
	if !strings.Contains(out.String(), "Restored files:") {
		t.Fatalf("restore output = %s", out.String())
	}
}

func TestWorkflowRequiresBackupDirAndConfirmation(t *testing.T) {
	workflow := NewWorkflow(Options{}, &bytes.Buffer{})
	if err := workflow.List(); err == nil || !strings.Contains(err.Error(), "--backup-dir is required") {
		t.Fatalf("List err = %v", err)
	}
	workflow = NewWorkflow(Options{BackupDir: t.TempDir()}, &bytes.Buffer{})
	if err := workflow.Restore("backup"); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("Restore err = %v", err)
	}
	if err := workflow.Cleanup("backup"); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("Cleanup err = %v", err)
	}
}
