package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePassphraseRejectsSymlinkAndOversizedFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "passphrase")
	if err := os.WriteFile(target, []byte("correct horse battery staple\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "passphrase-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := resolvePassphrase("", link); err == nil {
		t.Fatal("expected symlinked passphrase file to be rejected")
	}

	oversized := filepath.Join(dir, "oversized")
	if err := os.WriteFile(oversized, []byte(strings.Repeat("x", 64*1024+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolvePassphrase("", oversized); err == nil {
		t.Fatal("expected oversized passphrase file to be rejected")
	}
}
