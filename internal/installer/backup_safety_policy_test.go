package installer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupSafetyPolicyReturnsExistingOriginalPaths(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "existing")
	missing := filepath.Join(dir, "missing")
	if err := os.WriteFile(existing, []byte("old"), 0o600); err != nil {
		t.Fatalf("write existing: %v", err)
	}
	paths := NewBackupSafetyPolicy().ExistingOriginalPaths(backupManifest{Entries: []BackupEntry{
		{OriginalPath: existing},
		{OriginalPath: missing},
	}})
	if len(paths) != 1 || paths[0] != existing {
		t.Fatalf("paths = %+v", paths)
	}
}
