package backup

import (
	"errors"
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

func TestBackupManifestStoreLoadMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	_, err := NewBackupManifestStore(path).Load()
	if err == nil {
		t.Fatal("expected error for missing manifest")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got %v", err)
	}
}

func TestBackupManifestStoreSaveMarshalError(t *testing.T) {
	orig := manifestMarshal
	defer func() { manifestMarshal = orig }()
	manifestMarshal = func(any) ([]byte, error) {
		return nil, errors.New("injected marshal error")
	}
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := NewBackupManifestStore(path).Save(backupManifest{}); err == nil {
		t.Fatal("expected error")
	}
}
