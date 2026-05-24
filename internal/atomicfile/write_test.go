package atomicfile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteCreatesParentsAndSetsFileMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "file.txt")
	if err := Write(path, []byte("body"), 0o640, 0o750); err != nil {
		t.Fatalf("Write: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(body) != "body" {
		t.Fatalf("body = %q", body)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat file: %v", err)
	}
	if runtime.GOOS != "windows" {
		if fileInfo.Mode().Perm() != 0o640 {
			t.Fatalf("file mode = %o", fileInfo.Mode().Perm())
		}
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	if runtime.GOOS != "windows" {
		if dirInfo.Mode().Perm() != 0o750 {
			t.Fatalf("dir mode = %o", dirInfo.Mode().Perm())
		}
	}
}
