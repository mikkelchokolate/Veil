package managedfiles

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteFileDoesNotFollowStaleTmpSymlink(t *testing.T) {
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
		t.Fatalf("stale tmp symlink was followed: sentinel=%q", sent)
	}
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "replacement" {
		t.Fatalf("target=%q", body)
	}
	if info, err := os.Lstat(tmp); err == nil && info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("predictable tmp path was reused as a regular staging file")
	}
}

func TestWriteFileResetsStaleTmpMode(t *testing.T) {
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
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "secret" {
		t.Fatalf("target=%q", body)
	}
}

func TestWriteFileSucceedsWhenPredictableTmpIsDirectory(t *testing.T) {
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
}
