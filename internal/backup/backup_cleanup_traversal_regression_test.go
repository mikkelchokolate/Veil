package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanupBackupRejectsTraversalIDs(t *testing.T) {
	root := t.TempDir()
	backupDir := filepath.Join(root, "backups")
	fixtureID := "20240101_120000"
	fixture := filepath.Join(backupDir, fixtureID)
	if err := os.MkdirAll(fixture, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture, "data"), []byte("backup\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(root, "state.json")
	if err := os.WriteFile(sentinel, []byte("live state\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	lifecycle := NewLifecycle(backupDir)
	ids := []string{
		"..",
		".",
		"",
		filepath.Join("..", "state.json"),
		".." + string(filepath.Separator) + "state.json",
		filepath.Join(root, "state.json"),
		fixtureID + string(filepath.Separator) + "..",
	}
	for _, id := range ids {
		if err := lifecycle.Cleanup(id); err == nil {
			t.Fatalf("Cleanup(%q) succeeded", id)
		} else if !strings.Contains(strings.ToLower(err.Error()), "invalid backup id") {
			t.Fatalf("Cleanup(%q) error = %v, want invalid backup id", id, err)
		}
		if _, err := os.Stat(sentinel); err != nil {
			t.Fatalf("Cleanup(%q) removed sentinel: %v", id, err)
		}
		if _, err := os.Stat(filepath.Join(fixture, "data")); err != nil {
			t.Fatalf("Cleanup(%q) removed fixture: %v", id, err)
		}
		if _, err := os.Stat(backupDir); err != nil {
			t.Fatalf("Cleanup(%q) removed backup dir: %v", id, err)
		}
	}
}

func TestRestoreRejectsTraversalBackupIDs(t *testing.T) {
	root := t.TempDir()
	backupDir := filepath.Join(root, "backups")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(root, "state.json")
	if err := os.WriteFile(sentinel, []byte("live state\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	lifecycle := NewLifecycle(backupDir)
	for _, id := range []string{"..", ".", "", filepath.Join("..", "state.json")} {
		if _, err := lifecycle.Restore(id); err == nil {
			t.Fatalf("Restore(%q) succeeded", id)
		} else if !strings.Contains(strings.ToLower(err.Error()), "invalid backup id") {
			t.Fatalf("Restore(%q) error = %v, want invalid backup id", id, err)
		}
		body, err := os.ReadFile(sentinel)
		if err != nil {
			t.Fatalf("Restore(%q) removed sentinel: %v", id, err)
		}
		if string(body) != "live state\n" {
			t.Fatalf("Restore(%q) mutated sentinel: %q", id, body)
		}
	}
}

func TestCleanupBackupRejectsSymlinkBackupID(t *testing.T) {
	root := t.TempDir()
	backupDir := filepath.Join(root, "backups")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(outside, "secret")
	if err := os.WriteFile(secret, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	id := "20240101_120000"
	if err := os.Symlink(outside, filepath.Join(backupDir, id)); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	err := NewLifecycle(backupDir).Cleanup(id)
	if err == nil {
		t.Fatal("Cleanup of symlink backup ID succeeded")
	}
	if _, err := os.Stat(secret); err != nil {
		t.Fatalf("symlink target was removed: %v", err)
	}
}

func TestCleanupBackupStillRemovesValidChild(t *testing.T) {
	root := t.TempDir()
	backupDir := filepath.Join(root, "backups")
	src := filepath.Join(root, "veil.env")
	if err := os.WriteFile(src, []byte("cfg"), 0o600); err != nil {
		t.Fatal(err)
	}
	lifecycle := NewLifecycle(backupDir)
	id, err := lifecycle.BackupExisting([]string{src})
	if err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Cleanup(id); err != nil {
		t.Fatalf("Cleanup valid ID: %v", err)
	}
	if _, err := os.Stat(filepath.Join(backupDir, id)); !os.IsNotExist(err) {
		t.Fatalf("valid backup still present: %v", err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("source removed: %v", err)
	}
}
