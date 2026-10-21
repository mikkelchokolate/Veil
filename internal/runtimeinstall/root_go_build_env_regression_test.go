package runtimeinstall

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func envMap(t *testing.T, env []string) map[string]string {
	t.Helper()
	m := map[string]string{}
	for _, e := range env {
		key, value, ok := strings.Cut(e, "=")
		if !ok {
			continue
		}
		if _, dup := m[key]; dup {
			t.Fatalf("duplicate %s in build env: %q", key, env)
		}
		m[key] = value
	}
	return m
}

func withRootHome(t *testing.T) string {
	t.Helper()
	// Go may write telemetry/cache state under HOME with modes t.TempDir's
	// strict cleanup trips on, so use a plain temp dir.
	home, err := os.MkdirTemp("", "veil-1337-roothome-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = filepath.Walk(home, func(path string, info os.FileInfo, err error) error {
			if err == nil {
				_ = os.Chmod(path, 0o755)
			}
			return nil
		})
		_ = os.RemoveAll(home)
	})
	old := rootHomeFn
	rootHomeFn = func() string { return home }
	t.Cleanup(func() { rootHomeFn = old })
	return home
}

// Issue #1337: under root the go build env is allowlisted. Ambient GOFLAGS,
// GOENV/HOME/XDG_*, module/build caches, C toolchain and git hooks, and every
// checksum-database bypass are dropped; only proxies/locale and GOPROXY pass,
// and sum.golang.org verification is forced on.
func TestRootGoBuildEnvDropsAmbientBuildControls(t *testing.T) {
	asRoot(t)
	home := withRootHome(t)
	poisoned := map[string]string{
		"GOFLAGS":           "-toolexec=/tmp/evil",
		"GOENV":             "/tmp/evil.env",
		"HOME":              "/home/attacker",
		"XDG_CONFIG_HOME":   "/home/attacker/.config",
		"XDG_CACHE_HOME":    "/home/attacker/.cache",
		"GOPATH":            "/tmp/attacker-gopath",
		"GOMODCACHE":        "/tmp/attacker-modcache",
		"GOCACHE":           "/tmp/attacker-gocache",
		"CC":                "/tmp/evil-cc",
		"CGO_CFLAGS":        "-fplugin=/tmp/evil.so",
		"GIT_SSH_COMMAND":   "/tmp/evil-ssh",
		"GIT_CONFIG_GLOBAL": "/tmp/evil.gitconfig",
		"GONOSUMDB":         "*",
		"GONOSUMCHECK":      "1",
		"GOSUMDB":           "off",
		"GOPRIVATE":         "*",
		"GONOPROXY":         "*",
		"GOINSECURE":        "*",
		"GOVCS":             "*:all",
		"GOTOOLCHAIN":       "go1.99.0",
		"GOROOT":            "/tmp/attacker-goroot",
		"PATH":              "/tmp/attacker-bin:/usr/bin",
	}
	for k, v := range poisoned {
		t.Setenv(k, v)
	}
	t.Setenv("GOPROXY", "https://goproxy.example")
	t.Setenv("HTTPS_PROXY", "http://egress.example:3128")
	t.Setenv("LC_ALL", "C.UTF-8")

	m := envMap(t, goBuildEnv(context.Background(), "/usr/local/go/bin/go"))

	for k, v := range poisoned {
		if m[k] == v {
			t.Errorf("%s=%q inherited into the root build env", k, v)
		}
	}
	for _, k := range []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "CC", "CGO_CFLAGS", "GIT_SSH_COMMAND", "GIT_CONFIG_GLOBAL"} {
		if v, ok := m[k]; ok {
			t.Errorf("%s=%q must not be present in the root build env", k, v)
		}
	}
	want := map[string]string{
		"HOME":         home,
		"GOENV":        "off",
		"GOFLAGS":      "",
		"GOPATH":       filepath.Join(home, "go"),
		"GOMODCACHE":   filepath.Join(home, "go", "pkg", "mod"),
		"GOCACHE":      filepath.Join(home, ".cache", "go-build"),
		"GOTOOLCHAIN":  "local",
		"GOSUMDB":      "sum.golang.org",
		"GONOSUMDB":    "",
		"GONOSUMCHECK": "",
		"GOPRIVATE":    "",
		"GOINSECURE":   "",
		"GOBIN":        "/usr/local/go/bin",
		"GOPROXY":      "https://goproxy.example",
		"HTTPS_PROXY":  "http://egress.example:3128",
		"LC_ALL":       "C.UTF-8",
	}
	for k, v := range want {
		if got, ok := m[k]; !ok || got != v {
			t.Errorf("%s = %q (present=%v), want %q", k, got, ok, v)
		}
	}
	if strings.Contains(m["PATH"], "/tmp/attacker-bin") {
		t.Errorf("PATH = %q inherits the ambient PATH", m["PATH"])
	}
}

// Issue #1337 end to end: a real `go build` with the root build env must not
// run a -toolexec shim supplied through GOFLAGS or through a go env file
// under the ambient HOME.
func TestRootGoBuildDoesNotRunAmbientToolexec(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go toolchain on PATH")
	}
	if testing.Short() {
		t.Skip("builds a module with a cold cache")
	}
	asRoot(t)
	withRootHome(t)
	work := t.TempDir()
	marker := filepath.Join(work, "PWNED")
	shim := filepath.Join(work, "evil")
	if err := os.WriteFile(shim, []byte("#!/bin/sh\n: > '"+marker+"'\nexec \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	attackerHome := filepath.Join(work, "attacker-home")
	if err := os.MkdirAll(filepath.Join(attackerHome, ".config", "go"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(attackerHome, ".config", "go", "env"), []byte("GOFLAGS=-toolexec="+shim+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mod := filepath.Join(work, "mod")
	if err := os.MkdirAll(mod, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mod, "go.mod"), []byte("module example.com/veil1337\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mod, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for name, setup := range map[string]func(*testing.T){
		"GOFLAGS":     func(t *testing.T) { t.Setenv("GOFLAGS", "-toolexec="+shim) },
		"go env file": func(t *testing.T) { t.Setenv("HOME", attackerHome); t.Setenv("XDG_CONFIG_HOME", "") },
	} {
		t.Run(name, func(t *testing.T) {
			// Subtest-scoped Setenv: each case tests exactly one vector.
			setup(t)
			_ = os.Remove(marker)
			cmd := exec.Command(goBin, "build", "-o", filepath.Join(work, "out"), ".")
			cmd.Dir = mod
			cmd.Env = append(goBuildEnv(context.Background(), goBin), "CGO_ENABLED=0")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("go build: %v\n%s", err, out)
			}
			if _, err := os.Stat(marker); err == nil {
				t.Fatalf("ambient -toolexec shim ran during the root build (%s)", name)
			}
		})
	}
}

// Issue #1337: no build mode disables checksum verification by default any
// more; the old code appended GONOSUMDB=* whenever it was unset.
func TestGoBuildEnvNoBlanketGONOSUMDB(t *testing.T) {
	t.Setenv("GONOSUMDB", "")
	os.Unsetenv("GONOSUMDB")
	for name, mode := range map[string]func(*testing.T){"root": asRoot, "unprivileged": asUnprivileged} {
		t.Run(name, func(t *testing.T) {
			mode(t)
			if name == "root" {
				withRootHome(t)
			}
			for _, e := range goBuildEnv(context.Background(), "/usr/local/go/bin/go") {
				if e == "GONOSUMDB=*" {
					t.Fatalf("%s build env disables sumdb for every module: %q", name, e)
				}
			}
		})
	}
}

// #1341 review: the production root-home lookup (not the test stub) is what
// defeats the ambient-HOME vector, so pin it directly.
func TestRootAccountHomeIgnoresAmbientHOME(t *testing.T) {
	ambient := t.TempDir()
	t.Setenv("HOME", ambient)
	got := rootAccountHome()
	if got == ambient || !filepath.IsAbs(got) || got == "/" {
		t.Fatalf("rootAccountHome() = %q with HOME=%q; want root's account home", got, ambient)
	}
}

// #1341 review: the toolchain probes (go version / go env GOROOT, whose
// output becomes GOROOT= in the root build env) must not read a go env file
// under root.
func TestGoLocalEnvDisablesGoEnvFileUnderRoot(t *testing.T) {
	has := func() bool {
		for _, e := range goLocalEnv() {
			if e == "GOENV=off" {
				return true
			}
		}
		return false
	}
	t.Run("root", func(t *testing.T) {
		asRoot(t)
		if !has() {
			t.Fatal("goLocalEnv under root lacks GOENV=off")
		}
	})
	t.Run("unprivileged", func(t *testing.T) {
		asUnprivileged(t)
		if has() {
			t.Fatal("goLocalEnv unprivileged must keep the user's go env file")
		}
	})
}

// #1341 review: an ambient GOSUMDB survives under root only if it still
// verifies sum.golang.org with Go's built-in key (a mirror URL is fine).
func TestRootGOSUMDBKeepsOnlyBuiltinKeyedSumDB(t *testing.T) {
	for in, want := range map[string]string{
		"":               "sum.golang.org",
		"off":            "sum.golang.org",
		"sum.golang.org": "sum.golang.org",
		"sum.golang.org https://mirror.example/sumdb/sum.golang.org": "sum.golang.org https://mirror.example/sumdb/sum.golang.org",
		"sum.golang.org http://mirror.example":                       "sum.golang.org",
		"sum.golang.org+deadbeef+AAAA":                               "sum.golang.org",
		"evil.example+1234+AAAA https://evil":                        "sum.golang.org",
		"sum.golang.org https://a https://b":                         "sum.golang.org",
	} {
		if got := rootGOSUMDB(in); got != want {
			t.Errorf("rootGOSUMDB(%q) = %q, want %q", in, got, want)
		}
	}
	asRoot(t)
	withRootHome(t)
	t.Setenv("GOSUMDB", "sum.golang.org https://mirror.example/sumdb/sum.golang.org")
	if got := envMap(t, goBuildEnv(context.Background(), "/usr/local/go/bin/go"))["GOSUMDB"]; got != "sum.golang.org https://mirror.example/sumdb/sum.golang.org" {
		t.Fatalf("root build env GOSUMDB = %q, want the builtin-keyed mirror kept", got)
	}
}
