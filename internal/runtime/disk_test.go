package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiskEndpointHasKnownPaths(t *testing.T) {
	// Create temp dirs to simulate veil paths
	dir := t.TempDir()
	etcDir := filepath.Join(dir, "etc", "veil")
	varDir := filepath.Join(dir, "var", "lib", "veil")
	os.MkdirAll(etcDir, 0o755)
	os.MkdirAll(varDir, 0o755)
	os.WriteFile(filepath.Join(etcDir, "test.conf"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(varDir, "state.json"), []byte("world"), 0o644)

	stats := DirSize(dir)
	if len(stats) == 0 {
		t.Error("expected at least one directory stat")
	}
	// Find the entry for our dir
	sizes := make(map[string]int64)
	for _, s := range stats {
		sizes[s.Path] = s.SizeBytes
	}
	// Check that etc subdirectory has positive size
	etcPath := filepath.Join(dir, "etc")
	if v, ok := sizes[etcPath]; !ok || v <= 0 {
		t.Errorf("expected positive size for %s, got %d", etcPath, v)
	}
}

func TestDiskEndpointFieldsPresent(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "f.txt"), []byte("data"), 0o644)
	stats := DirSize(dir)
	if len(stats) == 0 {
		t.Fatal("expected at least one subdirectory stat")
	}
	first := stats[0]
	if first.Path == "" {
		t.Error("expected non-empty path")
	}
	if first.SizeBytes < 0 {
		t.Errorf("expected non-negative size, got %d", first.SizeBytes)
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
