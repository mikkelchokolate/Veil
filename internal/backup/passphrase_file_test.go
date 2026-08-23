package backup

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEnsurePassphraseFileCreatesOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backup.passphrase")
	if err := EnsurePassphraseFile(path); err != nil {
		t.Fatalf("EnsurePassphraseFile: %v", err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pass := strings.TrimRight(string(first), "\r\n")
	if len(pass) < MinPassphraseLength {
		t.Fatalf("generated passphrase too short: %q", pass)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("passphrase must not be group/world accessible, mode %o", info.Mode().Perm())
	}
	if err := EnsurePassphraseFile(path); err != nil {
		t.Fatalf("second EnsurePassphraseFile: %v", err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("EnsurePassphraseFile rotated an existing passphrase")
	}
}

func TestWriteNewPassphraseFileRejectsEmptyPath(t *testing.T) {
	if _, err := WriteNewPassphraseFile(""); err == nil {
		t.Fatal("expected empty path error")
	}
}
