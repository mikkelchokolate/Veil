package backup

import (
	"path/filepath"
	"reflect"
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
	// The complete manifest entry must round-trip — OriginalPath, BackupPath,
	// and Size alike — not merely the original path.
	if !reflect.DeepEqual(loaded.Entries, manifest.Entries) {
		t.Fatalf("loaded entries = %+v, want %+v", loaded.Entries, manifest.Entries)
	}
}
