package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupManifestStoreSaveErrors(t *testing.T) {
	dir := t.TempDir()
	// Path is a directory, so writing the manifest file fails.
	path := filepath.Join(dir, "manifest.json")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := NewBackupManifestStore(path).Save(backupManifest{}); err == nil {
		t.Fatal("expected error when manifest path is a directory")
	}
}

func TestBackupManifestStoreLoadErrors(t *testing.T) {
	t.Run("read error", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "manifest.json")
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := NewBackupManifestStore(path).Load(); err == nil {
			t.Fatal("expected error when manifest path is a directory")
		}
	})

	t.Run("unmarshal error", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "manifest.json")
		if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := NewBackupManifestStore(path).Load(); err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})
}
