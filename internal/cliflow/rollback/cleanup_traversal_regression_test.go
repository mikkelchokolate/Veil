package rollback

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkflowCleanupRejectsTraversalID(t *testing.T) {
	root := t.TempDir()
	backupDir := filepath.Join(root, "backups")
	if err := os.MkdirAll(filepath.Join(backupDir, "20240101_120000"), 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(root, "state.json")
	if err := os.WriteFile(sentinel, []byte("live state\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err := NewWorkflow(Options{BackupDir: backupDir, Yes: true}, &out).Cleanup("..")
	if err == nil {
		t.Fatal("expected cleanup of traversal ID to fail")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "invalid backup id") {
		t.Fatalf("error = %v, want invalid backup id", err)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("sentinel removed: %v", err)
	}
	if _, err := os.Stat(backupDir); err != nil {
		t.Fatalf("backup dir removed: %v", err)
	}
}
