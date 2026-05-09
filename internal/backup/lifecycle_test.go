package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLifecycleBacksUpListsRestoresAndCleansUp(t *testing.T) {
	root := t.TempDir()
	managed := filepath.Join(root, "veil.env")
	if err := os.WriteFile(managed, []byte("before"), 0o600); err != nil {
		t.Fatalf("write managed: %v", err)
	}
	backupDir := filepath.Join(root, "backups")
	lifecycle := NewLifecycle(backupDir)
	backupID, err := lifecycle.BackupExisting([]string{managed})
	if err != nil {
		t.Fatalf("BackupExisting: %v", err)
	}
	ids, err := lifecycle.List()
	if err != nil || len(ids) != 1 || ids[0] != backupID {
		t.Fatalf("List = %+v err=%v", ids, err)
	}
	if err := os.WriteFile(managed, []byte("after"), 0o600); err != nil {
		t.Fatalf("modify managed: %v", err)
	}
	restored, err := lifecycle.Restore(backupID)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	body, _ := os.ReadFile(managed)
	if len(restored) != 1 || string(body) != "before" {
		t.Fatalf("restored=%+v body=%q", restored, string(body))
	}
	if err := lifecycle.Cleanup(backupID); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
}
