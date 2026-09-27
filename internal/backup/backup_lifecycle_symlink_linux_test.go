//go:build linux

package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// #1131: the backup root typically lives under the service-owned
// /var/lib/veil, so a "backups" leaf swapped for a symlink must fail closed —
// root must never create the backup dir and drop copies under an
// attacker-chosen target.

func TestBackupExistingRefusesSymlinkedBackupRoot(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	backupsDir := filepath.Join(base, "backups")
	if err := os.Symlink(outside, backupsDir); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(base, "src.conf")
	if err := os.WriteFile(src, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	lifecycle := NewLifecycle(backupsDir)
	_, err := lifecycle.BackupExisting([]string{src})
	if err == nil {
		t.Fatal("BackupExisting followed a symlinked backup root")
	}
	// Nothing must have landed in the outside target.
	entries, rErr := os.ReadDir(outside)
	if rErr != nil {
		t.Fatal(rErr)
	}
	if len(entries) != 0 {
		t.Fatalf("backup material escaped into the symlink target: %v", entries)
	}
}

func TestBackupExistingCreatesRootDescriptorRelative(t *testing.T) {
	base := t.TempDir()
	backupsDir := filepath.Join(base, "backups")
	src := filepath.Join(base, "src.conf")
	if err := os.WriteFile(src, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	lifecycle := NewLifecycle(backupsDir)
	id, err := lifecycle.BackupExisting([]string{src})
	if err != nil {
		t.Fatalf("BackupExisting: %v", err)
	}
	manifestPath := filepath.Join(backupsDir, id, backupManifestName)
	if _, err := os.Lstat(manifestPath); err != nil {
		t.Fatalf("manifest not written under the pinned backup dir: %v", err)
	}
	info, err := os.Lstat(backupsDir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		t.Fatal("backup root is not a real directory")
	}
}

// A source swapped for a symlink after the stat must not be read through.
func TestBackupExistingRefusesSymlinkedSource(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	victim := filepath.Join(outside, "victim")
	if err := os.WriteFile(victim, []byte("root-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(base, "src.conf")
	if err := os.Symlink(victim, src); err != nil {
		t.Fatal(err)
	}

	lifecycle := NewLifecycle(filepath.Join(base, "backups"))
	_, err := lifecycle.BackupExisting([]string{src})
	if err == nil {
		t.Fatal("BackupExisting copied through a symlinked source")
	}
	if !strings.Contains(err.Error(), src) {
		t.Fatalf("error should name the offending source: %v", err)
	}
}
