package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteCleansUpAndDoesNotCommitOnSyncFailure(t *testing.T) {
	origSync := syncFile
	syncFile = func(*os.File) error { return errors.New("injected sync error") }
	defer func() { syncFile = origSync }()

	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := Write(path, []byte("body"), 0o640, 0o750); err == nil {
		t.Fatal("expected error when temp file sync fails")
	}
	assertNoTempFiles(t, dir)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target file should not exist: %v", err)
	}
}
