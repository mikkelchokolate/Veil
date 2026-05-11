package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceBinaryFromArchiveBacksUpAndReplacesCurrentBinary(t *testing.T) {
	dir := t.TempDir()
	currentPath := filepath.Join(dir, "veil")
	if err := os.WriteFile(currentPath, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	archive := createTestTarGz(t, "veil", []byte("new-binary"))

	backupPath, err := ReplaceBinaryFromArchive(currentPath, archive, true)
	if err != nil {
		t.Fatalf("ReplaceBinaryFromArchive: %v", err)
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

func TestReplaceBinaryFromArchiveRequiresConfirmation(t *testing.T) {
	dir := t.TempDir()
	currentPath := filepath.Join(dir, "veil")
	if err := os.WriteFile(currentPath, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	archive := createTestTarGz(t, "veil", []byte("new-binary"))

	_, err := ReplaceBinaryFromArchive(currentPath, archive, false)
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

func createTestTarGz(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
