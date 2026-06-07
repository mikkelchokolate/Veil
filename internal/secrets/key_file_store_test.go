package secrets

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestKeyFileStoreCreatesAndReloadsKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.key")
	store := NewKeyFileStore(path)
	key, err := store.LoadOrCreate()
	if err != nil {
		t.Fatalf("LoadOrCreate create: %v", err)
	}
	if len(key[:]) != KeySize {
		t.Fatalf("key length = %d", len(key[:]))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if runtime.GOOS != "windows" {
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("mode = %o", info.Mode().Perm())
		}
	}
	loaded, err := store.LoadOrCreate()
	if err != nil {
		t.Fatalf("LoadOrCreate load: %v", err)
	}
	if *loaded != *key {
		t.Fatal("loaded key differs")
	}
}

func TestKeyFileStoreAcceptsGroupReadableKey(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission test")
	}
	path := filepath.Join(t.TempDir(), "state.key")
	var raw [KeySize]byte
	for i := range raw {
		raw[i] = byte(i)
	}
	// 0640 is the deployed mode (root:veil) so the group-scoped veil service can
	// read a root-owned key; LoadOrCreate must accept it without error or chmod.
	if err := os.WriteFile(path, raw[:], 0o640); err != nil {
		t.Fatal(err)
	}
	store := NewKeyFileStore(path)
	loaded, err := store.LoadOrCreate()
	if err != nil {
		t.Fatalf("LoadOrCreate with 0640 key: %v", err)
	}
	if *loaded != raw {
		t.Fatal("loaded key differs from written key")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("0640 key must be preserved, got %#o", got)
	}
}
