package repair

import (
	"os"
	"path/filepath"
	"testing"
)

// #1262: the caddy path repair renders into veil-caddy.service must not come
// from the ambient PATH of a root-run repair.
func TestDefaultLookPathIgnoresAmbientPATH(t *testing.T) {
	dir := t.TempDir()
	shim := filepath.Join(dir, "caddy")
	if err := os.WriteFile(shim, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if got := (PlanDependencies{}).resolvedBinaryPath("caddy"); got == shim {
		t.Fatalf("resolvedBinaryPath(caddy) = PATH shim %q", got)
	}
}
