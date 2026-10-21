package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/syspath"
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

// Review follow-up on #1331: an Env override that carries PATH (for example
// append(os.Environ(), "HOME=...") from RunFixedConfigValidation) must not put
// the ambient PATH back; the scrub has to run after the merge.
func TestRuntimeCommandExecutorScrubsPATHFromEnvOverrides(t *testing.T) {
	poisoned := t.TempDir()
	t.Setenv("PATH", poisoned+string(os.PathListSeparator)+os.Getenv("PATH"))
	for name, env := range map[string][]string{
		"ambient env":   append(os.Environ(), "HOME=/tmp"),
		"explicit PATH": {"PATH=" + poisoned, "HOME=/tmp"},
	} {
		child := NewRuntimeCommandExecutor().Run(RuntimeCommandInput{
			Command: []string{"sh", "-c", "printf '%s|%s' \"$PATH\" \"$HOME\""},
			Env:     env,
		})
		if child.Err != nil {
			t.Fatalf("%s: sh: %v", name, child.Err)
		}
		path, home, _ := strings.Cut(child.Output, "|")
		if path != syspath.TrustedPATH() {
			t.Fatalf("%s: child PATH = %q, want trusted %q", name, path, syspath.TrustedPATH())
		}
		if home != "/tmp" {
			t.Fatalf("%s: child HOME = %q, want non-PATH override kept", name, home)
		}
	}
}
