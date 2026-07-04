package backup

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func makeCircularSymlink(t *testing.T, dir, name string) string {
	t.Helper()
	a := filepath.Join(dir, name+"a")
	b := filepath.Join(dir, name+"b")
	if err := os.Symlink(filepath.Base(b), a); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(a), b); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestBackupLifecycleBackupExistingErrors(t *testing.T) {
	t.Run("mkdir backup dir fails", func(t *testing.T) {
		dir := t.TempDir()
		// backupDir is a file, so MkdirAll on its parent will not fail, but
		// MkdirAll(backupDir) itself will fail because it's not a directory.
		backupDir := filepath.Join(dir, "backups")
		if err := os.WriteFile(backupDir, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		src := filepath.Join(dir, "src.txt")
		if err := os.WriteFile(src, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := NewBackupLifecycle(backupDir).BackupExisting([]string{src}); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("stat source fails", func(t *testing.T) {
		dir := t.TempDir()
		backupDir := filepath.Join(dir, "backups")
		src := makeCircularSymlink(t, dir, "src")
		if _, err := NewBackupLifecycle(backupDir).BackupExisting([]string{src}); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestBackupLifecycleRestoreErrors(t *testing.T) {
	t.Run("backup dir stat fails", func(t *testing.T) {
		dir := t.TempDir()
		backupDir := makeCircularSymlink(t, dir, "backup")
		if _, err := NewBackupLifecycle(backupDir).Restore("id"); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("backup is not a directory", func(t *testing.T) {
		dir := t.TempDir()
		backupDir := filepath.Join(dir, "backups")
		if err := os.MkdirAll(backupDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(backupDir, "id"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := NewBackupLifecycle(backupDir).Restore("id"); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("manifest load error", func(t *testing.T) {
		dir := t.TempDir()
		backupDir := filepath.Join(dir, "backups")
		backupID := "20260101_120000"
		backupPath := filepath.Join(backupDir, backupID)
		if err := os.MkdirAll(backupPath, 0o700); err != nil {
			t.Fatal(err)
		}
		// manifest.json as directory triggers a non-ENOENT read error.
		if err := os.MkdirAll(filepath.Join(backupPath, "manifest.json"), 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := NewBackupLifecycle(backupDir).Restore(backupID); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("missing backup file", func(t *testing.T) {
		dir := t.TempDir()
		backupDir := filepath.Join(dir, "backups")
		managed := filepath.Join(dir, "veil.env")
		if err := os.WriteFile(managed, []byte("before"), 0o600); err != nil {
			t.Fatal(err)
		}
		lifecycle := NewBackupLifecycle(backupDir)
		backupID, err := lifecycle.BackupExisting([]string{managed})
		if err != nil {
			t.Fatal(err)
		}
		// Remove the backed-up file so stat fails during restore.
		entries, err := os.ReadDir(filepath.Join(backupDir, backupID))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.Name() != "manifest.json" {
				if err := os.Remove(filepath.Join(backupDir, backupID, e.Name())); err != nil {
					t.Fatal(err)
				}
			}
		}
		if _, err := lifecycle.Restore(backupID); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("mkdir original parent fails", func(t *testing.T) {
		dir := t.TempDir()
		backupDir := filepath.Join(dir, "backups")
		parentDir := filepath.Join(dir, "parent")
		managed := filepath.Join(parentDir, "veil.env")
		if err := os.MkdirAll(parentDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(managed, []byte("before"), 0o600); err != nil {
			t.Fatal(err)
		}
		lifecycle := NewBackupLifecycle(backupDir)
		backupID, err := lifecycle.BackupExisting([]string{managed})
		if err != nil {
			t.Fatal(err)
		}
		// Turn parent into a file so restoring has to fail when recreating it.
		if err := os.RemoveAll(parentDir); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(parentDir, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := lifecycle.Restore(backupID); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestBackupLifecycleListReadDirError(t *testing.T) {
	dir := t.TempDir()
	// Use a file path as the backup dir; ReadDir returns ENOTDIR.
	backupDir := filepath.Join(dir, "backups")
	if err := os.WriteFile(backupDir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewBackupLifecycle(backupDir).List(); err == nil {
		t.Fatal("expected error")
	}
}

func TestBackupLifecycleBackupExistingDirectorySource(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	id, err := NewBackupLifecycle(backupDir).BackupExisting([]string{srcDir})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(backupDir, id))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "manifest.json" {
			t.Fatalf("directory source should be skipped, found %q", e.Name())
		}
	}
}

func TestBackupLifecycleBackupExistingCopyError(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	src := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(src, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	orig := fileCopierCopy
	defer func() { fileCopierCopy = orig }()
	fileCopierCopy = func(io.Writer, io.Reader) (int64, error) {
		return 0, errors.New("injected copy error")
	}

	if _, err := NewBackupLifecycle(backupDir).BackupExisting([]string{src}); err == nil {
		t.Fatal("expected error for copy failure")
	}
}

func TestBackupLifecycleRestoreCopyError(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	managed := filepath.Join(dir, "veil.env")
	if err := os.WriteFile(managed, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}

	lifecycle := NewBackupLifecycle(backupDir)
	backupID, err := lifecycle.BackupExisting([]string{managed})
	if err != nil {
		t.Fatal(err)
	}

	// Remove the original so the restore loop itself fails instead of the safety backup.
	if err := os.Remove(managed); err != nil {
		t.Fatal(err)
	}

	orig := fileCopierCopy
	defer func() { fileCopierCopy = orig }()
	fileCopierCopy = func(io.Writer, io.Reader) (int64, error) {
		return 0, errors.New("injected copy error")
	}

	if _, err := lifecycle.Restore(backupID); err == nil {
		t.Fatal("expected error for copy failure")
	}
}

func TestBackupLifecycleBackupExistingMkdirAllError(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	src := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(src, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	orig := lifecycleMkdirAll
	defer func() { lifecycleMkdirAll = orig }()
	lifecycleMkdirAll = func(string, os.FileMode) error {
		return errors.New("injected mkdir error")
	}

	if _, err := NewBackupLifecycle(backupDir).BackupExisting([]string{src}); err == nil {
		t.Fatal("expected error for mkdir failure")
	}
}

func TestBackupLifecycleBackupExistingManifestSaveError(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	src := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(src, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	orig := lifecycleManifestSave
	defer func() { lifecycleManifestSave = orig }()
	lifecycleManifestSave = func(string, Manifest) error {
		return errors.New("injected manifest save error")
	}

	if _, err := NewBackupLifecycle(backupDir).BackupExisting([]string{src}); err == nil {
		t.Fatal("expected error for manifest save failure")
	}
}

func TestBackupLifecycleRestoreSafetyBackupError(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	managed := filepath.Join(dir, "veil.env")
	if err := os.WriteFile(managed, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}

	lifecycle := NewBackupLifecycle(backupDir)
	backupID, err := lifecycle.BackupExisting([]string{managed})
	if err != nil {
		t.Fatal(err)
	}

	orig := lifecycleManifestSave
	defer func() { lifecycleManifestSave = orig }()
	lifecycleManifestSave = func(string, Manifest) error {
		return errors.New("injected safety manifest save error")
	}

	if _, err := lifecycle.Restore(backupID); err == nil {
		t.Fatal("expected error for safety backup failure")
	}
}

func TestBackupLifecycleRestoreParentMkdirError(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	backupID := "20260101_120000"
	backupPath := filepath.Join(backupDir, backupID)
	if err := os.MkdirAll(backupPath, 0o700); err != nil {
		t.Fatal(err)
	}

	originalParent := filepath.Join(dir, "parentfile")
	originalPath := filepath.Join(originalParent, "managed.env")
	backupFile := filepath.Join(backupPath, "managed.env")
	if err := os.WriteFile(backupFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	manifest := Manifest{
		Entries: []Entry{{
			OriginalPath: originalPath,
			BackupPath:   backupFile,
			Size:         1,
		}},
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupPath, "manifest.json"), manifestData, 0o600); err != nil {
		t.Fatal(err)
	}
	// Turn the parent path into a file so MkdirAll fails during restore.
	if err := os.WriteFile(originalParent, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := NewBackupLifecycle(backupDir).Restore(backupID); err == nil {
		t.Fatal("expected error for parent mkdir failure")
	}
}

func TestBackupLifecycleRestoreMissingManifest(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backups")
	backupID := "20260101_120000"
	backupPath := filepath.Join(backupDir, backupID)
	if err := os.MkdirAll(backupPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := NewBackupLifecycle(backupDir).Restore(backupID); err == nil {
		t.Fatal("expected error for missing manifest")
	}
}
