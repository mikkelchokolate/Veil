package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func init() {
	// fsync is extremely slow under the race detector and is not necessary for
	// the unit tests in this package.
	fileCopierSync = func(*os.File) error { return nil }
}

func TestBackupFileCopierCopyErrors(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")

	t.Run("missing source", func(t *testing.T) {
		if err := NewBackupFileCopier().Copy(src, dst, 0o600); err == nil {
			t.Fatal("expected error for missing source")
		}
	})

	t.Run("destination is directory", func(t *testing.T) {
		if err := os.WriteFile(src, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(dst, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := NewBackupFileCopier().Copy(src, dst, 0o600); err == nil {
			t.Fatal("expected error when destination is a directory")
		}
	})

	t.Run("destination parent is file", func(t *testing.T) {
		if err := os.WriteFile(src, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		parentFile := filepath.Join(dir, "parent")
		if err := os.WriteFile(parentFile, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := NewBackupFileCopier().Copy(src, filepath.Join(parentFile, "dst"), 0o600); err == nil {
			t.Fatal("expected error when destination parent is a file")
		}
	})
}
