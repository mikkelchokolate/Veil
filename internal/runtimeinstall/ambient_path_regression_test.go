package runtimeinstall

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/syspath"
)

// plantGoShim writes a fake `go` that records its execution and claims a very
// new version, the shape that used to win findBestGo's newest-version pick.
func plantGoShim(t *testing.T, dir string) string {
	t.Helper()
	marker := filepath.Join(dir, "PWNED")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	shim := "#!/bin/sh\n: > '" + marker + "'\necho 'go version go1.99.0 linux/amd64'\n"
	if err := os.WriteFile(filepath.Join(dir, "go"), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	return marker
}

func asRoot(t *testing.T) {
	t.Helper()
	old := runningAsRootFn
	runningAsRootFn = func() bool { return true }
	t.Cleanup(func() { runningAsRootFn = old })
}

func asUnprivileged(t *testing.T) {
	t.Helper()
	old := runningAsRootFn
	runningAsRootFn = func() bool { return false }
	t.Cleanup(func() { runningAsRootFn = old })
}

// Issue #1332: under root the toolchain must not come from ambient PATH,
// HOME or GOROOT, and no such candidate may even be executed for probing.
func TestKnownGoBinsIgnoresAmbientEnvUnderRoot(t *testing.T) {
	asRoot(t)
	pathDir := filepath.Join(t.TempDir(), "path")
	// The real toolchain probe may write Go telemetry state under HOME, so
	// HOME is not a t.TempDir (whose strict cleanup would flake on that).
	home, err := os.MkdirTemp("", "veil-1332-home-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	goroot := t.TempDir()
	pathMarker := plantGoShim(t, pathDir)
	homeMarker := plantGoShim(t, filepath.Join(home, "go", "bin"))
	gorootMarker := plantGoShim(t, filepath.Join(goroot, "bin"))
	t.Setenv("PATH", pathDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", home)
	t.Setenv("GOROOT", goroot)

	for _, candidate := range knownGoBins() {
		for _, bad := range []string{pathDir, home, goroot} {
			if strings.HasPrefix(candidate, bad) {
				t.Fatalf("knownGoBins returned ambient candidate %q under root", candidate)
			}
		}
	}
	got, err := findBestGo(defaultGoVersion)
	if err != nil {
		t.Fatalf("findBestGo: %v", err)
	}
	for _, bad := range []string{pathDir, home, goroot} {
		if got != "" && strings.HasPrefix(got, bad) {
			t.Fatalf("findBestGo selected planted toolchain %q", got)
		}
	}
	for _, m := range []string{pathMarker, homeMarker, gorootMarker} {
		if _, err := os.Stat(m); err == nil {
			t.Fatalf("planted go shim was executed during probing (%s)", m)
		}
	}
}

// Unprivileged runs keep the developer-friendly ambient lookup.
func TestKnownGoBinsKeepsAmbientEnvWhenUnprivileged(t *testing.T) {
	asUnprivileged(t)
	pathDir := t.TempDir()
	plantGoShim(t, pathDir)
	t.Setenv("PATH", pathDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	found := false
	for _, c := range knownGoBins() {
		if c == filepath.Join(pathDir, "go") {
			found = true
		}
	}
	if !found {
		t.Fatalf("unprivileged knownGoBins dropped the PATH go")
	}
}

func TestGoBuildEnvUsesTrustedPATHUnderRoot(t *testing.T) {
	asRoot(t)
	t.Setenv("PATH", "/tmp/evil:/usr/bin")
	var pathEntry string
	for _, e := range goBuildEnv(context.Background(), "/usr/local/go/bin/go") {
		if strings.HasPrefix(e, "PATH=") {
			pathEntry = e
		}
	}
	want := "PATH=" + syspath.TrustedPATH() + ":/usr/local/go/bin"
	if pathEntry != want {
		t.Fatalf("PATH = %q, want %q", pathEntry, want)
	}
}

func TestRunCaddyNaiveBuildRejectsAmbientBareGo(t *testing.T) {
	pathDir := t.TempDir()
	marker := plantGoShim(t, pathDir)
	t.Setenv("PATH", pathDir)
	err := runCaddyNaiveBuild(context.Background(), "go-veil-not-in-trusted-dirs", t.TempDir(), filepath.Join(t.TempDir(), "caddy"))
	if err == nil || !strings.Contains(err.Error(), "resolve go toolchain") {
		t.Fatalf("err = %v, want trusted-dir resolution failure", err)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("planted go was executed")
	}
}
