package installer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupLifecycleBacksUpListsRestoresAndCleansUp(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	managed := filepath.Join(dir, "veil.env")
	if err := os.WriteFile(managed, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	lifecycle := NewBackupLifecycle(backupDir)
	backupID, err := lifecycle.BackupExisting([]string{managed})
	if err != nil {
		t.Fatalf("BackupExisting: %v", err)
	}
	ids, err := lifecycle.List()
	if err != nil || len(ids) != 1 || ids[0] != backupID {
		t.Fatalf("List = %+v, %v", ids, err)
	}
	if err := os.WriteFile(managed, []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	restored, err := lifecycle.Restore(backupID)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	body, err := os.ReadFile(restored[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "before" {
		t.Fatalf("restored body = %q", string(body))
	}
	if err := lifecycle.Cleanup(backupID); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
}
