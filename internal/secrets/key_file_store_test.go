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
