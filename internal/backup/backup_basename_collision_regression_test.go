package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupExistingKeepsDistinctSameBasenameFiles(t *testing.T) {
	root := t.TempDir()
	backupDir := filepath.Join(root, "backups")
	aDir := filepath.Join(root, "a")
	bDir := filepath.Join(root, "b")
	if err := os.MkdirAll(aDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(bDir, 0o755); err != nil {
		t.Fatal(err)
	}
	aPath := filepath.Join(aDir, "config.json")
	bPath := filepath.Join(bDir, "config.json")
	if err := os.WriteFile(aPath, []byte("first-config"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bPath, []byte("second-config"), 0o640); err != nil {
		t.Fatal(err)
	}

	lifecycle := NewLifecycle(backupDir)
	id, err := lifecycle.BackupExisting([]string{aPath, bPath, aPath})
	if err != nil {
		t.Fatalf("BackupExisting: %v", err)
	}

	manifestBody, err := os.ReadFile(filepath.Join(backupDir, id, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestBody, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Entries) != 2 {
		t.Fatalf("entries = %d, want 2 (duplicate source skipped)", len(manifest.Entries))
	}
	if manifest.Entries[0].BackupPath == manifest.Entries[1].BackupPath {
		t.Fatalf("colliding BackupPath %q", manifest.Entries[0].BackupPath)
	}
	if filepath.Base(manifest.Entries[0].BackupPath) == "config.json" && filepath.Base(manifest.Entries[1].BackupPath) == "config.json" {
		t.Fatal("both members used the colliding basename")
	}

	if err := os.WriteFile(aPath, []byte("changed-a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bPath, []byte("changed-b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Restore(id); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	gotA, err := os.ReadFile(aPath)
	if err != nil {
		t.Fatal(err)
	}
	gotB, err := os.ReadFile(bPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotA) != "first-config" || string(gotB) != "second-config" {
		t.Fatalf("restored a=%q b=%q; want first-config / second-config", gotA, gotB)
	}

	ids, err := lifecycle.List()
	if err != nil {
		t.Fatal(err)
	}
	var safetyID string
	for _, listed := range ids {
		if listed != id {
			safetyID = listed
			break
		}
	}
	if safetyID == "" {
		t.Fatal("expected a safety backup")
	}
	safetyBody, err := os.ReadFile(filepath.Join(backupDir, safetyID, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var safety Manifest
	if err := json.Unmarshal(safetyBody, &safety); err != nil {
		t.Fatal(err)
	}
	if len(safety.Entries) != 2 {
		t.Fatalf("safety entries = %d, want 2", len(safety.Entries))
	}
	if safety.Entries[0].BackupPath == safety.Entries[1].BackupPath {
		t.Fatalf("safety backup colliding BackupPath %q", safety.Entries[0].BackupPath)
	}
}
