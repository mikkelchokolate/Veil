package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRestoreRejectsBackupMemberOutsideBackupDir(t *testing.T) {
	root := t.TempDir()
	backupDir := filepath.Join(root, "backups")
	id := "20240101_120000"
	backupPath := filepath.Join(backupDir, id)
	if err := os.MkdirAll(backupPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupPath, "0_member"), []byte("member"), 0o600); err != nil {
		t.Fatal(err)
	}

	outsideSrc := filepath.Join(root, "outside-src")
	outsideDst := filepath.Join(root, "outside-dst")
	if err := os.WriteFile(outsideSrc, []byte("attacker-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outsideDst, []byte("keep-me"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeRestoreManifest(t, backupPath, Manifest{Entries: []Entry{{
		OriginalPath: outsideDst,
		BackupPath:   outsideSrc,
		Size:         int64(len("attacker-bytes")),
	}}})

	_, err := NewLifecycle(backupDir).Restore(id)
	if err == nil {
		t.Fatal("expected restore to reject escaped backup member")
	}
	body, readErr := os.ReadFile(outsideDst)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(body) != "keep-me" {
		t.Fatalf("destination mutated: %q", body)
	}
}

func TestRestoreRejectsBackupMemberTraversal(t *testing.T) {
	root := t.TempDir()
	backupDir := filepath.Join(root, "backups")
	id := "20240101_120000"
	backupPath := filepath.Join(backupDir, id)
	if err := os.MkdirAll(backupPath, 0o700); err != nil {
		t.Fatal(err)
	}
	outsideSrc := filepath.Join(root, "outside-src")
	outsideDst := filepath.Join(root, "outside-dst")
	if err := os.WriteFile(outsideSrc, []byte("attacker-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outsideDst, []byte("keep-me"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeRestoreManifest(t, backupPath, Manifest{Entries: []Entry{{
		OriginalPath: outsideDst,
		BackupPath:   filepath.Join("..", "..", "outside-src"),
		Size:         1,
	}}})

	if _, err := NewLifecycle(backupDir).Restore(id); err == nil {
		t.Fatal("expected restore to reject traversal backup path")
	}
	body, err := os.ReadFile(outsideDst)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "keep-me" {
		t.Fatalf("destination mutated: %q", body)
	}
}

func TestRestoreRejectsSymlinkBackupMember(t *testing.T) {
	root := t.TempDir()
	backupDir := filepath.Join(root, "backups")
	id := "20240101_120000"
	backupPath := filepath.Join(backupDir, id)
	if err := os.MkdirAll(backupPath, 0o700); err != nil {
		t.Fatal(err)
	}
	outsideSrc := filepath.Join(root, "outside-src")
	outsideDst := filepath.Join(root, "outside-dst")
	if err := os.WriteFile(outsideSrc, []byte("attacker-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outsideDst, []byte("keep-me"), 0o600); err != nil {
		t.Fatal(err)
	}
	member := filepath.Join(backupPath, "0_member")
	if err := os.Symlink(outsideSrc, member); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}
	writeRestoreManifest(t, backupPath, Manifest{Entries: []Entry{{
		OriginalPath: outsideDst,
		BackupPath:   member,
		Size:         1,
	}}})

	if _, err := NewLifecycle(backupDir).Restore(id); err == nil {
		t.Fatal("expected restore to reject symlink backup member")
	}
	body, err := os.ReadFile(outsideDst)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "keep-me" {
		t.Fatalf("destination mutated: %q", body)
	}
}

func TestRestoreRejectsManifestJSONAsBackupMember(t *testing.T) {
	root := t.TempDir()
	backupDir := filepath.Join(root, "backups")
	id := "20240101_120000"
	backupPath := filepath.Join(backupDir, id)
	if err := os.MkdirAll(backupPath, 0o700); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(root, "outside-dst")
	if err := os.WriteFile(dst, []byte("keep-me"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeRestoreManifest(t, backupPath, Manifest{Entries: []Entry{{
		OriginalPath: dst,
		BackupPath:   filepath.Join(backupPath, "manifest.json"),
		Size:         1,
	}}})

	if _, err := NewLifecycle(backupDir).Restore(id); err == nil {
		t.Fatal("expected restore to reject manifest.json as backup data")
	}
	body, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "keep-me" {
		t.Fatalf("destination mutated: %q", body)
	}
}

func TestRestoreRejectsDuplicateDestinations(t *testing.T) {
	root := t.TempDir()
	backupDir := filepath.Join(root, "backups")
	id := "20240101_120000"
	backupPath := filepath.Join(backupDir, id)
	if err := os.MkdirAll(backupPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupPath, "0_a"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupPath, "1_b"), []byte("b"), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(root, "outside-dst")
	if err := os.WriteFile(dst, []byte("keep-me"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeRestoreManifest(t, backupPath, Manifest{Entries: []Entry{
		{OriginalPath: dst, BackupPath: "0_a", Size: 1},
		{OriginalPath: dst, BackupPath: "1_b", Size: 1},
	}})

	if _, err := NewLifecycle(backupDir).Restore(id); err == nil {
		t.Fatal("expected restore to reject duplicate destinations")
	}
	body, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "keep-me" {
		t.Fatalf("destination mutated: %q", body)
	}
}

func TestRestoreRejectsRelativeOriginalPath(t *testing.T) {
	root := t.TempDir()
	backupDir := filepath.Join(root, "backups")
	id := "20240101_120000"
	backupPath := filepath.Join(backupDir, id)
	if err := os.MkdirAll(backupPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupPath, "0_member"), []byte("member"), 0o600); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(root, "state.json")
	if err := os.WriteFile(sentinel, []byte("keep-me"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeRestoreManifest(t, backupPath, Manifest{Entries: []Entry{{
		OriginalPath: filepath.Join("..", "state.json"),
		BackupPath:   "0_member",
		Size:         1,
	}}})

	if _, err := NewLifecycle(backupDir).Restore(id); err == nil {
		t.Fatal("expected restore to reject relative original path")
	}
	body, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "keep-me" {
		t.Fatalf("destination mutated: %q", body)
	}
}

func TestRestoreAcceptsLegacyAbsoluteMemberInsideBackup(t *testing.T) {
	root := t.TempDir()
	backupDir := filepath.Join(root, "backups")
	id := "20240101_120000"
	backupPath := filepath.Join(backupDir, id)
	if err := os.MkdirAll(backupPath, 0o700); err != nil {
		t.Fatal(err)
	}
	member := filepath.Join(backupPath, "0_veil.env")
	if err := os.WriteFile(member, []byte("from-backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(root, "veil.env")
	if err := os.WriteFile(dst, []byte("current"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeRestoreManifest(t, backupPath, Manifest{Entries: []Entry{{
		OriginalPath: dst,
		BackupPath:   member,
		Size:         int64(len("from-backup")),
	}}})

	restored, err := NewLifecycle(backupDir).Restore(id)
	if err != nil {
		t.Fatalf("Restore legacy absolute member: %v", err)
	}
	if len(restored) != 1 || restored[0] != dst {
		t.Fatalf("restored=%v", restored)
	}
	body, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "from-backup" {
		t.Fatalf("body=%q", body)
	}
}

func TestBackupExistingStoresRelativeMemberNames(t *testing.T) {
	root := t.TempDir()
	backupDir := filepath.Join(root, "backups")
	src := filepath.Join(root, "veil.env")
	if err := os.WriteFile(src, []byte("cfg"), 0o600); err != nil {
		t.Fatal(err)
	}
	id, err := NewLifecycle(backupDir).BackupExisting([]string{src})
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(backupDir, id, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Entries) != 1 {
		t.Fatalf("entries=%d", len(manifest.Entries))
	}
	if filepath.IsAbs(manifest.Entries[0].BackupPath) {
		t.Fatalf("BackupPath should be a relative member name, got %q", manifest.Entries[0].BackupPath)
	}
	if strings.Contains(manifest.Entries[0].BackupPath, "..") {
		t.Fatalf("BackupPath contains traversal: %q", manifest.Entries[0].BackupPath)
	}
}

func writeRestoreManifest(t *testing.T, backupPath string, manifest Manifest) {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupPath, "manifest.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}
