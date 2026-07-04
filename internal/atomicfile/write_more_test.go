package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteFailsWhenCreateTempFailsInjected(t *testing.T) {
	origCreateTemp := createTemp
	createTemp = func(dir, pattern string) (*os.File, error) {
		return nil, errors.New("injected create temp error")
	}
	defer func() { createTemp = origCreateTemp }()

	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := Write(path, []byte("body"), 0o644, 0o755); err == nil {
		t.Fatal("expected error when CreateTemp fails")
	}
	assertNoTempFiles(t, dir)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("target file should not exist: %v", err)
	}
}

func TestWriteFailsWhenCreateTempFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permission model differs on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("root can create files in read-only directories")
	}

	dir := t.TempDir()
	targetDir := filepath.Join(dir, "readonly")
	if err := os.Mkdir(targetDir, 0o000); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	defer os.Chmod(targetDir, 0o755)

	path := filepath.Join(targetDir, "file.txt")
	if err := Write(path, []byte("body"), 0o644, 0o755); err == nil {
		t.Fatal("expected error when target directory is not writable")
	}

	// Restore permissions so we can verify no temp file was left behind.
	if err := os.Chmod(targetDir, 0o755); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	assertNoTempFiles(t, targetDir)
}

func TestWriteCleansUpTempFileOnCloseFailure(t *testing.T) {
	origClose := closeFile
	closeFile = func(f *os.File) error {
		// Close the real descriptor so the temp file can be removed cleanly.
		_ = f.Close()
		return errors.New("injected close error")
	}
	defer func() { closeFile = origClose }()

	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := Write(path, []byte("body"), 0o644, 0o755); err == nil {
		t.Fatal("expected error when temp file close fails")
	}
	assertNoTempFiles(t, dir)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("target file should not exist: %v", err)
	}
}

func TestWriteCleansUpTempFileOnChmodFailure(t *testing.T) {
	origChmod := chmod
	chmod = func(name string, mode os.FileMode) error {
		return errors.New("injected chmod error")
	}
	defer func() { chmod = origChmod }()

	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := Write(path, []byte("body"), 0o644, 0o755); err == nil {
		t.Fatal("expected error when chmod fails")
	}
	assertNoTempFiles(t, dir)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("target file should not exist: %v", err)
	}
}
