package managedfiles

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// writeFile stages through atomicfile.Write, which creates a randomized
// ".tmp-*" sibling — the historical predictable "<target>.tmp" path is no
// longer consulted. These tests keep regression value by asserting the stale
// sibling is ignored (never opened, never removed) and that real staging
// leaves no leftovers; the symlink-safety of the randomized staging path
// itself is covered in internal/atomicfile.

func TestWriteFileIgnoresStaleTmpSiblingSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "managed.txt")
	sentinel := filepath.Join(dir, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("keep-me"), 0o600); err != nil {
		t.Fatal(err)
	}
	tmp := target + ".tmp"
	if err := os.Symlink(sentinel, tmp); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	if err := WriteFile(target, "replacement", 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	sent, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatal(err)
	}
	if string(sent) != "keep-me" {
		t.Fatalf("stale tmp sibling was followed: sentinel=%q", sent)
	}
	// The stale sibling must be left exactly as planted — ignored, not
	// unlinked or followed.
	info, err := os.Lstat(tmp)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("stale tmp sibling changed: info=%v err=%v", info, err)
	}
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "replacement" {
		t.Fatalf("target=%q", body)
	}
	assertNoStagingLeftovers(t, dir)
}

func TestWriteFilePublishesWithRequestedModeBesideStaleSibling(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission model differs on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "secret.env")
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(target, "secret", 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("published mode=%o want 0600", info.Mode().Perm())
	}
	// The stale sibling is unrelated state: it stays in place with its own
	// mode, and the randomized staging file is cleaned up after publish.
	stale, err := os.Stat(tmp)
	if err != nil || stale.Mode().Perm() != 0o644 {
		t.Fatalf("stale tmp sibling should be untouched, info=%v err=%v", stale, err)
	}
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "secret" {
		t.Fatalf("target=%q", body)
	}
	assertNoStagingLeftovers(t, dir)
}

func TestWriteFileIgnoresStaleTmpSiblingDirectory(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target+".tmp", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(target, "content", 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "content" {
		t.Fatalf("target=%q", body)
	}
	// The unrelated directory is still there and no randomized staging file
	// leaked into the directory.
	stale, err := os.Stat(target + ".tmp")
	if err != nil || !stale.IsDir() {
		t.Fatalf("stale tmp sibling directory should be untouched: info=%v err=%v", stale, err)
	}
	assertNoStagingLeftovers(t, dir)
}

// assertNoStagingLeftovers proves the randomized ".tmp-*" staging path was
// used and cleaned up — no staged file may survive a successful write.
func assertNoStagingLeftovers(t *testing.T, dir string) {
	t.Helper()
	leftovers, err := filepath.Glob(filepath.Join(dir, ".tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("staging leftovers remain after WriteFile: %v", leftovers)
	}
}
