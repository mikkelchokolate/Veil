package runtime

import (
	"path/filepath"
	"runtime"
	"testing"
)

func veilDirPaths(stats DiskStats) []string {
	paths := make([]string, 0, len(stats.Dirs))
	for _, d := range stats.Dirs {
		paths = append(paths, d.Path)
	}
	return paths
}

// /api/disk must follow VEIL_ETC_DIR/VEIL_VAR_DIR so a custom install reports
// its own tree instead of the packaged /etc/veil + /var/lib/veil pair
// (issue #638).
func TestDiskStatsFollowsConfiguredVeilDirs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX path semantics")
	}
	etcDir := t.TempDir()
	varDir := t.TempDir()
	t.Setenv("VEIL_ETC_DIR", etcDir)
	t.Setenv("VEIL_VAR_DIR", varDir)
	t.Setenv("VEIL_CADDY_STATE_DIR", "")
	t.Setenv("VEIL_MITA_STATE_DIR", "")
	// Detach the *_PATH fallbacks so hostenv does not re-derive a root.
	t.Setenv("VEIL_STATE_PATH", "")
	t.Setenv("VEIL_KEY_PATH", "")
	t.Setenv("VEIL_LIVE_ROOT", "")

	stats := readDirDiskStats()
	paths := veilDirPaths(stats)
	if len(paths) < 2 || paths[0] != varDir || paths[1] != etcDir {
		t.Fatalf("dirs = %v, want first entries %q, %q", paths, varDir, etcDir)
	}
	for _, p := range paths {
		if p == "/etc/veil" || p == "/var/lib/veil" {
			t.Fatalf("packaged dir %q reported for a custom install: %v", p, paths)
		}
	}
}

// Optional Caddy/Mita state directories appear when configured or present,
// and never duplicate (issue #638).
func TestDiskStatsIncludesOptionalStateDirs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX path semantics")
	}
	t.Setenv("VEIL_ETC_DIR", t.TempDir())
	t.Setenv("VEIL_VAR_DIR", t.TempDir())
	t.Setenv("VEIL_STATE_PATH", "")
	t.Setenv("VEIL_KEY_PATH", "")
	t.Setenv("VEIL_LIVE_ROOT", "")

	caddyDir := t.TempDir()
	// Explicitly configured dirs are listed even when empty; an unset Mita
	// dir that does not exist on this host stays out.
	t.Setenv("VEIL_CADDY_STATE_DIR", caddyDir)
	t.Setenv("VEIL_MITA_STATE_DIR", filepath.Join(t.TempDir(), "missing-mita"))

	stats := readDirDiskStats()
	paths := veilDirPaths(stats)
	seen := map[string]int{}
	for _, p := range paths {
		seen[filepath.Clean(p)]++
	}
	if seen[filepath.Clean(caddyDir)] != 1 {
		t.Fatalf("configured caddy state dir %q missing or duplicated: %v", caddyDir, paths)
	}
}

func TestDiskStatsDeduplicatesIdenticalRoots(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX path semantics")
	}
	shared := t.TempDir()
	t.Setenv("VEIL_ETC_DIR", shared)
	t.Setenv("VEIL_VAR_DIR", shared)
	t.Setenv("VEIL_STATE_PATH", "")
	t.Setenv("VEIL_KEY_PATH", "")
	t.Setenv("VEIL_LIVE_ROOT", "")
	t.Setenv("VEIL_CADDY_STATE_DIR", shared)

	stats := readDirDiskStats()
	count := 0
	for _, d := range stats.Dirs {
		if filepath.Clean(d.Path) == filepath.Clean(shared) {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("shared root reported %d times: %v", count, veilDirPaths(stats))
	}
}
