package installer

import (
	"path/filepath"
	"testing"
)

// #1130: runtime-shared classification must be a path-prefix match against
// the configured etc dir — not a substring scan. A custom layout whose
// systemd dir (or any unrelated ancestor) happens to contain "panel"/"tls"/
// "generated" must not mark unrelated material veil-proxy-shared.

func TestIsRuntimeSharedConfigMatchesConfiguredEtcDir(t *testing.T) {
	etcDir := t.TempDir()
	cases := []struct {
		path string
		want bool
	}{
		{filepath.Join(etcDir, "panel", "tls.crt"), true},
		{filepath.Join(etcDir, "generated", "caddy", "config.json"), true},
		{filepath.Join(etcDir, "tls", "cert.pem"), true},
		{filepath.Join(etcDir, "certs", "leaf.crt"), true},
		{filepath.Join(etcDir, "www", "index.html"), true},
		// The subtree root itself is shared.
		{filepath.Join(etcDir, "panel"), true},
		// Outside the configured etc dir entirely.
		{filepath.Join(t.TempDir(), "panel", "tls.crt"), false},
		// Same-named components under unrelated ancestors.
		{filepath.Join("/opt", "panel", "units", "generated", "x"), false},
		// Boundary: "panels" must not match "panel".
		{filepath.Join(etcDir, "panels", "tls.crt"), false},
		{filepath.Join(etcDir, "generated-old", "x"), false},
		// etcDir-local secrets are panel-only, not runtime-shared.
		{filepath.Join(etcDir, "veil.env"), false},
		{filepath.Join(etcDir, "state.key"), false},
	}
	for _, tc := range cases {
		if got := isRuntimeSharedConfig(tc.path, etcDir); got != tc.want {
			t.Fatalf("isRuntimeSharedConfig(%q, %q) = %v, want %v", tc.path, etcDir, got, tc.want)
		}
	}
}

func TestIsRuntimeSharedConfigEmptyEtcDirMatchesNothing(t *testing.T) {
	if isRuntimeSharedConfig(filepath.Join(string(filepath.Separator), "any", "panel", "x"), "") {
		t.Fatal("empty etcDir must not classify anything as runtime-shared")
	}
}

// A custom --systemd-dir nested under a directory named like a shared
// subtree used to classify the unit files as runtime-shared.
func TestDesiredFileOwnerIgnoresLookalikeAncestors(t *testing.T) {
	etcDir := filepath.Join(t.TempDir(), "etc-veil")
	systemdLookalike := filepath.Join(t.TempDir(), "panel", "systemd")
	unitPath := filepath.Join(systemdLookalike, "veil.service")
	owner := desiredFileOwner(unitPath, etcDir, 100, 101)
	if owner == nil || owner.GID != 0 {
		t.Fatalf("unit under a lookalike 'panel' ancestor got %+v, want root:root", owner)
	}
	// And the real shared subtree still resolves.
	shared := filepath.Join(etcDir, "generated", "mieru", "server_config.json")
	owner = desiredFileOwner(shared, etcDir, 100, 101)
	if owner == nil || owner.GID != 101 {
		t.Fatalf("shared config got %+v, want root:veil-proxy", owner)
	}
}

// runtimeSharedParentDirs must stop at the subtree root — the etc dir itself
// stays root:veil.
func TestRuntimeSharedParentDirsStopsAtSubtreeRoot(t *testing.T) {
	etcDir := t.TempDir()
	path := filepath.Join(etcDir, "generated", "caddy", "config.json")
	dirs := runtimeSharedParentDirs(path, etcDir)
	want := []string{
		filepath.Join(etcDir, "generated", "caddy"),
		filepath.Join(etcDir, "generated"),
	}
	if len(dirs) != len(want) {
		t.Fatalf("dirs=%v, want %v", dirs, want)
	}
	for i := range want {
		if dirs[i] != want[i] {
			t.Fatalf("dirs=%v, want %v", dirs, want)
		}
	}
	// A file outside every shared subtree yields no dirs at all.
	if got := runtimeSharedParentDirs(filepath.Join(etcDir, "veil.env"), etcDir); got != nil {
		t.Fatalf("unexpected parent dirs for non-shared file: %v", got)
	}
}
