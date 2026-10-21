package syspath

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// plantShim writes an executable shim named name into a fresh directory that
// creates marker when run, prepends that directory to PATH, and returns the
// shim path and marker path.
func plantShim(t *testing.T, name string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	marker := filepath.Join(dir, name+".ran")
	shim := filepath.Join(dir, name)
	if err := os.WriteFile(shim, []byte("#!/bin/sh\n: > '"+marker+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return shim, marker
}

func TestLookPathIgnoresAmbientPATH(t *testing.T) {
	shim, _ := plantShim(t, "veil-syspath-shim-tool")
	got, err := LookPath("veil-syspath-shim-tool")
	if err == nil {
		t.Fatalf("LookPath resolved %q through the ambient PATH (shim %q)", got, shim)
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestLookPathSearchesTrustedDirsInOrder(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	for _, dir := range []string{first, second} {
		if err := os.WriteFile(filepath.Join(dir, "tool"), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(first, "noexec"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(second, "noexec"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(first, "isdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := TrustedDirs
	TrustedDirs = []string{first, second}
	t.Cleanup(func() { TrustedDirs = old })

	if got, err := LookPath("tool"); err != nil || got != filepath.Join(first, "tool") {
		t.Fatalf("LookPath(tool) = %q, %v; want first trusted dir", got, err)
	}
	if got, err := LookPath("noexec"); err != nil || got != filepath.Join(second, "noexec") {
		t.Fatalf("LookPath(noexec) = %q, %v; want non-executable candidate skipped", got, err)
	}
	if _, err := LookPath("isdir"); err == nil {
		t.Fatal("LookPath accepted a directory")
	}
	if want := first + string(os.PathListSeparator) + second; TrustedPATH() != want {
		t.Fatalf("TrustedPATH() = %q, want %q", TrustedPATH(), want)
	}
}

func TestLookPathPathForms(t *testing.T) {
	shim, _ := plantShim(t, "veil-syspath-abs")
	if got, err := LookPath(shim); err != nil || got != shim {
		t.Fatalf("explicit absolute path = %q, %v; want accepted", got, err)
	}
	// Run from the shim's directory so "./name" exists and is executable:
	// only the relative-path rejection, not a failed stat, can refuse it.
	t.Chdir(filepath.Dir(shim))
	if _, err := LookPath("./" + filepath.Base(shim)); err == nil || !strings.Contains(err.Error(), "relative path") {
		t.Fatalf("relative path with separator err = %v, want relative-path rejection", err)
	}
	if _, err := LookPath(""); err == nil {
		t.Fatal("empty name must be rejected")
	}
}

func TestCommandDoesNotFallBackToAmbientPATH(t *testing.T) {
	_, marker := plantShim(t, "veil-syspath-shim-run")
	err := CommandContext(context.Background(), "veil-syspath-shim-run").Run()
	if err == nil || !errors.Is(err, ErrNotFound) {
		t.Fatalf("Run err = %v, want ErrNotFound", err)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("PATH shim was executed")
	}
}

func TestCommandResolvesTrustedToolAndScrubsChildPATH(t *testing.T) {
	trusted := t.TempDir()
	if err := os.WriteFile(filepath.Join(trusted, "printpath"), []byte("#!/bin/sh\nprintf '%s' \"$PATH\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := TrustedDirs
	TrustedDirs = []string{trusted, "/usr/bin", "/bin"}
	t.Cleanup(func() { TrustedDirs = old })
	t.Setenv("PATH", "/tmp/evil:"+os.Getenv("PATH"))

	cmd := Command("printpath")
	if cmd.Path != filepath.Join(trusted, "printpath") {
		t.Fatalf("cmd.Path = %q", cmd.Path)
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if string(out) != TrustedPATH() || strings.Contains(string(out), "/tmp/evil") {
		t.Fatalf("child PATH = %q, want %q", out, TrustedPATH())
	}
}

func TestEnvReplacesPATH(t *testing.T) {
	got := Env([]string{"A=1", "PATH=/evil", "B=2", "PATH=/evil2"})
	want := []string{"A=1", "B=2", "PATH=" + TrustedPATH()}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("Env = %q, want %q", got, want)
	}
}
