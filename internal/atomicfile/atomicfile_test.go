package atomicfile

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func TestWriteCreatesFileInExistingDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := Write(path, []byte("hello"), 0o640, 0o750); err != nil {
		t.Fatalf("Write: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(body) != "hello" {
		t.Fatalf("body = %q", body)
	}
}

func TestWriteOverwritesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := Write(path, []byte("new"), 0o600, 0o750); err != nil {
		t.Fatalf("Write: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(body) != "new" {
		t.Fatalf("body = %q", body)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("mode = %o", info.Mode().Perm())
		}
	}
}

func TestWriteEmptyBody(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	if err := Write(path, []byte{}, 0o644, 0o755); err != nil {
		t.Fatalf("Write: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(body) != 0 {
		t.Fatalf("body = %q", body)
	}
}

func TestWriteFailsWhenParentIsFile(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, "parent")
	if err := os.WriteFile(parent, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	path := filepath.Join(parent, "file.txt")
	if err := Write(path, []byte("body"), 0o644, 0o755); err == nil {
		t.Fatal("expected error when parent path is a file")
	}
}

func TestWriteFailsWhenDirectoryNotWritable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permission model differs on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("root can write to read-only directories")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	defer os.Chmod(dir, 0o755)

	path := filepath.Join(dir, "file.txt")
	if err := Write(path, []byte("body"), 0o644, 0o755); err == nil {
		t.Fatal("expected error when directory is not writable")
	}
}

func TestWriteCleansUpTempFileOnWriteFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("rlimit not supported on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")

	var rlimit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &rlimit); err != nil {
		t.Fatalf("Getrlimit: %v", err)
	}
	orig := rlimit.Cur
	rlimit.Cur = 0
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &rlimit); err != nil {
		t.Fatalf("Setrlimit: %v", err)
	}
	defer func() {
		rlimit.Cur = orig
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &rlimit); err != nil {
			t.Errorf("restore rlimit: %v", err)
		}
	}()

	if err := Write(path, []byte("body"), 0o644, 0o755); err == nil {
		t.Fatal("expected error when file size limit is 0")
	}
	assertNoTempFiles(t, dir)
}

func TestWriteCleansUpTempFileOnRenameFailure(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "existing"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := Write(target, []byte("body"), 0o644, 0o755); err == nil {
		t.Fatal("expected error when target is a non-empty directory")
	}
	assertNoTempFiles(t, dir)
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Fatalf("temp file was not cleaned up: %s", e.Name())
		}
	}
}
