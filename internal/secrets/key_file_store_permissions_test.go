package secrets

import (
	"errors"
	"os"
	"runtime"
	"testing"
)

func TestKeyFileStoreTightensWorldReadableKey(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission test")
	}
	path := tempKeyPath(t)
	var raw [KeySize]byte
	for i := range raw {
		raw[i] = byte(i + 1)
	}
	if err := os.WriteFile(path, raw[:], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := NewKeyFileStore(path).LoadOrCreate()
	if err != nil {
		t.Fatal(err)
	}
	if *loaded != raw {
		t.Fatal("loaded key differs from written key")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("hardened mode=%04o want 0600", got)
	}
}

func TestKeyFileStoreRejectsInsecureKeyWhenHardeningFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission test")
	}
	path := tempKeyPath(t)
	if err := os.WriteFile(path, make([]byte, KeySize), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	oldChmod := chmodKeyFile
	injected := errors.New("injected chmod failure")
	chmodKeyFile = func(string, os.FileMode) error { return injected }
	defer func() { chmodKeyFile = oldChmod }()

	if _, err := NewKeyFileStore(path).LoadOrCreate(); !errors.Is(err, injected) {
		t.Fatalf("error=%v want %v", err, injected)
	}
}
