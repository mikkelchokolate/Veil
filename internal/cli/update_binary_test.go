package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceVeilBinaryFromArchiveBacksUpAndReplacesCurrentBinary(t *testing.T) {
	dir := t.TempDir()
	currentPath := filepath.Join(dir, "veil")
	if err := os.WriteFile(currentPath, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	archive := createTarGz(t, "veil", []byte("new-binary"))

	backupPath, err := replaceVeilBinaryFromArchive(currentPath, archive, true)
	if err != nil {
		t.Fatalf("replaceVeilBinaryFromArchive: %v", err)
	}
	currentBody, err := os.ReadFile(currentPath)
	if err != nil {
		t.Fatal(err)
	}
	backupBody, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(currentBody) != "new-binary" || string(backupBody) != "old-binary" {
		t.Fatalf("unexpected current=%q backup=%q", string(currentBody), string(backupBody))
	}
}

func TestReplaceVeilBinaryFromArchiveRequiresConfirmation(t *testing.T) {
	dir := t.TempDir()
	currentPath := filepath.Join(dir, "veil")
	if err := os.WriteFile(currentPath, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	archive := createTarGz(t, "veil", []byte("new-binary"))

	_, err := replaceVeilBinaryFromArchive(currentPath, archive, false)
	if err == nil {
		t.Fatalf("expected confirmation error")
	}
	currentBody, err := os.ReadFile(currentPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(currentBody) != "old-binary" {
		t.Fatalf("binary changed without confirmation: %q", string(currentBody))
	}
}
