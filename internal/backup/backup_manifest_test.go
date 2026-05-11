package backup

import (
	"path/filepath"
	"testing"
)

func TestBackupManifestStoreWritesAndReadsManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	store := NewBackupManifestStore(path)
	manifest := backupManifest{Entries: []BackupEntry{{OriginalPath: "/etc/veil/veil.env", BackupPath: "/backup/veil.env", Size: 12}}}
	if err := store.Save(manifest); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Entries) != 1 || loaded.Entries[0].OriginalPath != manifest.Entries[0].OriginalPath {
		t.Fatalf("loaded = %+v", loaded)
	}
}
