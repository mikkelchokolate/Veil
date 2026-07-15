package secrets

import (
	"os"
	"runtime"
	"testing"
)

func TestKeyFileStoreAcceptsSecureReadOnlyModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission test")
	}
	for _, mode := range []os.FileMode{0o400, 0o440} {
		t.Run(mode.String(), func(t *testing.T) {
			path := tempKeyPath(t)
			var raw [KeySize]byte
			for i := range raw {
				raw[i] = byte(i + 11)
			}
			if err := os.WriteFile(path, raw[:], mode); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, mode); err != nil {
				t.Fatal(err)
			}
			oldChmod := chmodKeyFile
			chmodKeyFile = func(string, os.FileMode) error {
				t.Fatal("secure read-only key unexpectedly required chmod")
				return nil
			}
			defer func() { chmodKeyFile = oldChmod }()

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
			if got := info.Mode().Perm(); got != mode {
				t.Fatalf("mode=%04o want %04o", got, mode)
			}
		})
	}
}
