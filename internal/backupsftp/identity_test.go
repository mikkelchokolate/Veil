package backupsftp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadOrCreateInstallIDMintsPersistsAndReuses(t *testing.T) {
	idPath := filepath.Join(t.TempDir(), InstallIDFileName)
	id, err := loadOrCreateInstallID(idPath)
	if err != nil {
		t.Fatal(err)
	}
	if !installIDPattern.MatchString(id) {
		t.Fatalf("minted id %q is malformed", id)
	}
	// The file is root-only like the sibling status/known_hosts state.
	info, err := os.Stat(idPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("install id mode = %o, want 0600", info.Mode().Perm())
	}
	// Second call re-reads the same id instead of rotating it.
	again, err := loadOrCreateInstallID(idPath)
	if err != nil || again != id {
		t.Fatalf("reload = %q, %v", again, err)
	}
	if namespaceDir(id) != "veil-node-"+id {
		t.Fatalf("namespaceDir=%q", namespaceDir(id))
	}
}

func TestLoadOrCreateInstallIDAcceptsPersistedValue(t *testing.T) {
	dir := t.TempDir()
	idPath := filepath.Join(dir, InstallIDFileName)
	if err := os.WriteFile(idPath, []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	id, err := loadOrCreateInstallID(idPath)
	if err != nil || id != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("id=%q err=%v", id, err)
	}
}

func TestLoadOrCreateInstallIDRejectsMalformedFile(t *testing.T) {
	dir := t.TempDir()
	for _, body := range []string{"", "not-an-id", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "a]/evil", strings.Repeat("a", 33)} {
		idPath := filepath.Join(dir, InstallIDFileName)
		if err := os.WriteFile(idPath, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadOrCreateInstallID(idPath); err == nil || !strings.Contains(err.Error(), "malformed") {
			t.Fatalf("body %q accepted or wrong error: %v", body, err)
		}
		// A malformed file must not be silently replaced either.
		got, _ := os.ReadFile(idPath)
		if string(got) != body {
			t.Fatalf("malformed file overwritten: %q", got)
		}
	}
}

func TestLoadOrCreateInstallIDPropagatesReadErrors(t *testing.T) {
	dir := t.TempDir()
	// A directory at the id path is not a regular file: the error must
	// propagate instead of minting elsewhere.
	if err := os.MkdirAll(filepath.Join(dir, InstallIDFileName), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOrCreateInstallID(filepath.Join(dir, InstallIDFileName)); err == nil {
		t.Fatal("directory at id path accepted")
	}
}

func TestLoadOrCreateInstallIDCreatesParentDir(t *testing.T) {
	idPath := filepath.Join(t.TempDir(), "nested", "state", InstallIDFileName)
	id, err := loadOrCreateInstallID(idPath)
	if err != nil {
		t.Fatal(err)
	}
	if !installIDPattern.MatchString(id) {
		t.Fatalf("minted id %q is malformed", id)
	}
}
