package secrets

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestKeyFileStoreLoadOrCreateReadError(t *testing.T) {
	// Reading a directory returns an error that is not fs.ErrNotExist.
	store := NewKeyFileStore(t.TempDir())
	_, err := store.LoadOrCreate()
	if err == nil {
		t.Fatal("expected error when path is a directory")
	}
	if errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected non-ErrNotExist error, got %v", err)
	}
}

func TestKeyFileStoreCreateRandReadError(t *testing.T) {
	old := randRead
	randRead = func(b []byte) (int, error) { return 0, errors.New("injected rand.Read error") }
	defer func() { randRead = old }()

	store := NewKeyFileStore(filepath.Join(t.TempDir(), "state.key"))
	_, err := store.LoadOrCreate()
	if err == nil {
		t.Fatal("expected error when rand.Read fails")
	}
}

func TestKeyFileStoreCreateWriteError(t *testing.T) {
	root := t.TempDir()
	blockedParent := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(blockedParent, []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewKeyFileStore(filepath.Join(blockedParent, "state.key"))
	_, err := store.LoadOrCreate()
	if err == nil {
		t.Fatal("expected error when key file cannot be written")
	}
}
