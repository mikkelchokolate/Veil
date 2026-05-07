package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/veil-panel/veil/internal/installer"
)

func TestRollbackWorkflowListsAndRestoresBackup(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	managed := filepath.Join(dir, "veil.env")
	if err := os.WriteFile(managed, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	backupID, err := installer.NewBackupLifecycle(backupDir).BackupExisting([]string{managed})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managed, []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	workflow := NewRollbackWorkflow(rollbackWorkflowOptions{BackupDir: backupDir, Yes: true}, &out)
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
