package hostaccess

import (
	"os"
	"path/filepath"
	"testing"
)

// #1262: useradd/groupadd/chown helpers run as root during install must not
// exec a shim planted earlier in PATH.
func TestDefaultAccountDependenciesRunIgnoresAmbientPATHShims(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	if err := os.WriteFile(filepath.Join(dir, "veil-account-shim-1262"), []byte("#!/bin/sh\n: > '"+marker+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := DefaultAccountDependencies().Run("veil-account-shim-1262"); err == nil {
		t.Fatal("expected not-found error")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("PATH shim was executed")
	}
}
