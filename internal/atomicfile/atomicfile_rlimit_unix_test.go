//go:build unix

package atomicfile

import (
	"path/filepath"
	"syscall"
	"testing"
)

func TestWriteCleansUpTempFileOnWriteFailure(t *testing.T) {
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
