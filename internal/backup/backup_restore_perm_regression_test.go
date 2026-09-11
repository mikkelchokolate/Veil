package backup

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRestoreReplacesExistingFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes are not preserved on Windows")
	}
	root := t.TempDir()
	backupDir := filepath.Join(root, "backups")
	managed := filepath.Join(root, "veil.env")
	if err := os.WriteFile(managed, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(managed, 0o600); err != nil {
		t.Fatal(err)
	}

	lifecycle := NewLifecycle(backupDir)
	id, err := lifecycle.BackupExisting([]string{managed})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(managed, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Restore(id); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	info, err := os.Stat(managed)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("restored mode=%04o, want 0600", info.Mode().Perm())
	}
	body, err := os.ReadFile(managed)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "secret" {
		t.Fatalf("restored body=%q", body)
	}
}

func TestFileCopierAppliesModeToExistingDestination(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes are not preserved on Windows")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dst, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := NewFileCopier().Copy(src, dst, 0o600); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%04o, want 0600", info.Mode().Perm())
	}
	body, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "secret" {
		t.Fatalf("body=%q", body)
	}
}
