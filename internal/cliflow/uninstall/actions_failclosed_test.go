package uninstall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRemovePathRefusesUnmarkedDirectory is the #1025 regression: --etc-dir/
// --var-dir/--caddy-state-dir/--mita-state-dir reach os.RemoveAll verbatim,
// so a directory that is neither Veil-marked nor Veil-named must fail closed
// instead of being wiped.
func TestRemovePathRefusesUnmarkedDirectory(t *testing.T) {
	host := t.TempDir()
	hostile := filepath.Join(host, "documents")
	if err := os.MkdirAll(hostile, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hostile, "thesis.txt"), []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	called := false
	actions := NewActions(nil, func(path string) error {
		called = true
		return nil
	})
	err := actions.RemovePath(hostile)
	if err == nil {
		t.Fatal("RemovePath must refuse an unmarked non-empty directory")
	}
	if !strings.Contains(err.Error(), "not Veil-managed") {
		t.Fatalf("error should explain the refusal: %v", err)
	}
	if called {
		t.Fatal("file remover must not run for a refused path")
	}
	if _, statErr := os.Stat(filepath.Join(hostile, "thesis.txt")); statErr != nil {
		t.Fatalf("refused path content must survive: %v", statErr)
	}
}

// TestRemovePathAllowsVeilManagedTrees locks the #1025 allowlist: a real
// uninstall must still remove directories that carry Veil markers, carry a
// Veil-managed basename, or are empty.
func TestRemovePathAllowsVeilManagedTrees(t *testing.T) {
	host := t.TempDir()
	cases := map[string]func(dir string){
		// Marker file proves the tree belongs to Veil.
		"marked": func(dir string) {
			if err := os.WriteFile(filepath.Join(dir, "veil.env"), []byte("VEIL_API_TOKEN=x\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		// Runtime state layouts (.local/.config, certificates, acme).
		"runtime-state": func(dir string) {
			if err := os.MkdirAll(filepath.Join(dir, ".local"), 0o755); err != nil {
				t.Fatal(err)
			}
		},
		// Empty half-created custom dirs are still removed.
		"empty": func(dir string) {},
	}
	for name, plant := range cases {
		dir := filepath.Join(host, "opt", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		plant(dir)
		if err := ensureRemovablePath(dir); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	// Veil-managed basenames are provisioned by install/drop-ins even without
	// a marker file.
	for _, base := range []string{"veil", "caddy", "mita", "veil-backup.service.d"} {
		dir := filepath.Join(host, "named", base)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "unit.conf"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := ensureRemovablePath(dir); err != nil {
			t.Fatalf("basename %s: %v", base, err)
		}
	}
	// Non-directories and absent paths are single-node removals — allowed.
	regular := filepath.Join(host, "unit-file.service")
	if err := os.WriteFile(regular, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureRemovablePath(regular); err != nil {
		t.Fatalf("regular file: %v", err)
	}
	link := filepath.Join(host, "link")
	if err := os.Symlink(regular, link); err == nil {
		if err := ensureRemovablePath(link); err != nil {
			t.Fatalf("symlink: %v", err)
		}
	}
	if err := ensureRemovablePath(filepath.Join(host, "absent")); err != nil {
		t.Fatalf("absent path: %v", err)
	}
}

// TestRemovePathRefusesFilesystemRoot pins the worst-case #1025 guard:
// `--var-dir /` must never reach os.RemoveAll.
func TestRemovePathRefusesFilesystemRoot(t *testing.T) {
	if err := ensureRemovablePath(string(filepath.Separator)); err == nil {
		t.Fatal("filesystem root must be refused")
	}
}
