package secrets

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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

// The previous version of this test put a regular file where the key's parent
// directory belonged, so LoadOrCreate failed at os.Stat (ENOTDIR) and never
// reached the write path it claimed to cover. Injecting at the sync seam
// exercises the real create path: temp file, write, failed flush, no publish.
func TestKeyFileStoreCreateWriteError(t *testing.T) {
	old := syncKeyFile
	syncKeyFile = func(*os.File) error { return errors.New("injected sync error") }
	defer func() { syncKeyFile = old }()

	store := NewKeyFileStore(filepath.Join(t.TempDir(), "state.key"))
	_, err := store.LoadOrCreate()
	if err == nil {
		t.Fatal("expected error when key file cannot be written")
	}
	if !strings.Contains(err.Error(), "sync temporary key file") {
		t.Fatalf("error did not come from the write/publish path: %v", err)
	}
	// A failed publish must not leave a usable key file behind.
	if _, statErr := os.Stat(store.Path); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("key file exists after failed publish: %v", statErr)
	}
}
