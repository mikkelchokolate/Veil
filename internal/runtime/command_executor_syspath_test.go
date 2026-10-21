package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

// #1262: the uninstall executor must not resolve commands through a poisoned
// PATH, and its children must not inherit that PATH either.
func TestRuntimeCommandExecutorIgnoresAmbientPATHShims(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	if err := os.WriteFile(filepath.Join(dir, "veil-runtime-shim-1262"), []byte("#!/bin/sh\n: > '"+marker+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	out := NewRuntimeCommandExecutor().Run(RuntimeCommandInput{Command: []string{"veil-runtime-shim-1262"}})
	if !out.NotFound || out.Err == nil {
		t.Fatalf("result = %+v, want NotFound", out)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("PATH shim was executed")
	}

	child := NewRuntimeCommandExecutor().Run(RuntimeCommandInput{Command: []string{"sh", "-c", "printf '%s' \"$PATH\""}})
	if child.Err != nil {
		t.Fatalf("sh: %v", child.Err)
	}
	for _, entry := range filepath.SplitList(child.Output) {
		if entry == dir {
			t.Fatalf("child inherited poisoned PATH %q", child.Output)
		}
	}
}
