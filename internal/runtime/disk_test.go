package runtime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/hostenv"
)

// TestDirSizeReportsSubdirectoryTotals exercises the DirSize helper — the
// /api/disk endpoint itself is covered in internal/api (#931/#932 keep helper
// tests honestly named).
func TestDirSizeReportsSubdirectoryTotals(t *testing.T) {
	dir := t.TempDir()
	etcDir := filepath.Join(dir, "etc", "veil")
	varDir := filepath.Join(dir, "var", "lib", "veil")
	if err := os.MkdirAll(etcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(varDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(etcDir, "test.conf"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(varDir, "state.json"), []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}

	stats := DirSize(dir)
	if len(stats) != 2 {
		t.Fatalf("expected etc+var entries, got %+v", stats)
	}
	sizes := make(map[string]DirSizeInfo)
	for _, s := range stats {
		sizes[s.Path] = s
	}
	for _, sub := range []string{"etc", "var"} {
		entry, ok := sizes[filepath.Join(dir, sub)]
		if !ok {
			t.Fatalf("missing %s entry: %+v", sub, stats)
		}
		if entry.SizeBytes != 5 || entry.SizeHuman != "5 B" {
			t.Fatalf("%s size = %d (%q), want 5 (5 B)", sub, entry.SizeBytes, entry.SizeHuman)
		}
	}
}

func TestDirSizeFieldsArePopulated(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "f.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	stats := DirSize(dir)
	if len(stats) != 1 {
		t.Fatalf("expected one subdirectory stat, got %+v", stats)
	}
	first := stats[0]
	if first.Path != sub {
		t.Errorf("path = %q, want %q", first.Path, sub)
	}
	if first.SizeBytes != 4 || first.SizeHuman != "4 B" {
		t.Errorf("size = %d (%q), want 4 (4 B)", first.SizeBytes, first.SizeHuman)
	}
}

// #641: /var/log is not Veil-managed; walking it on every GET /api/disk made
// the endpoint expensive and mixed system log volume into the Veil disk card.
// #638: the managed set follows the configured etc/var roots plus the
// optional Caddy/Mita state dirs — nothing else may leak in.
func TestVeilDirsOnlyCoversVeilManagedPaths(t *testing.T) {
	allowed := map[string]bool{
		hostenv.VarDir(): true,
		hostenv.EtcDir(): true,
		"/var/lib/caddy": true,
		"/var/lib/mita":  true,
	}
	for _, dir := range veilDirs() {
		if !allowed[dir] {
			t.Fatalf("veilDirs contains non-Veil-managed path %q", dir)
		}
	}
}

func TestFormatBytesFormatsSizes(t *testing.T) {
	cases := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
		{1536, "1.5 KB"},
	}
	for _, tc := range cases {
		got := formatBytes(tc.bytes)
		if got != tc.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tc.bytes, got, tc.want)
		}
	}
}

func TestDirSizeRecursiveIgnoresErrors(t *testing.T) {
	size := dirSizeRecursive("/nonexistent/veil-runtime-test-path")
	if size != 0 {
		t.Fatalf("expected 0 for missing path, got %d", size)
	}
}

func TestDirSizeReturnsEmptyForInvalidRoot(t *testing.T) {
	stats := DirSize("/nonexistent/veil-runtime-test-path")
	if len(stats) != 0 {
		t.Fatalf("expected empty result, got %+v", stats)
	}
}

func TestDirSizeSkipsNonDirectoryEntries(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("data"), 0o644)
	stats := DirSize(dir)
	if len(stats) != 0 {
		t.Fatalf("expected no directory entries, got %+v", stats)
	}
}
