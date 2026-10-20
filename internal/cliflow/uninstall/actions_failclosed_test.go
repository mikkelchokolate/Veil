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

// TestRemovePathAllowsVeilManagedTrees locks the #1025/#1093 allowlist: a
// real uninstall must still remove directories that carry Veil markers,
// carry a Veil-managed basename, or are empty.
func TestRemovePathAllowsVeilManagedTrees(t *testing.T) {
	host := t.TempDir()
	cases := map[string]func(dir string){
		// Marker file proves the tree belongs to Veil.
		"marked": func(dir string) {
			if err := os.WriteFile(filepath.Join(dir, "veil.env"), []byte("VEIL_API_TOKEN=x\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		// The install/repair sentinel is the canonical ownership proof.
		"sentinel": func(dir string) {
			if err := os.WriteFile(filepath.Join(dir, ".veil-managed"), []byte(""), 0o644); err != nil {
				t.Fatal(err)
			}
		},
		// The state database marks a Veil var dir.
		"database": func(dir string) {
			if err := os.WriteFile(filepath.Join(dir, "veil.db"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		// state.key and backup.passphrase are the remaining markers.
		"statekey": func(dir string) {
			if err := os.WriteFile(filepath.Join(dir, "state.key"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"passphrase": func(dir string) {
			if err := os.WriteFile(filepath.Join(dir, "backup.passphrase"), []byte("x"), 0o600); err != nil {
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
	// a marker file — this is also what blesses runtime-state layouts: a
	// caddy/mita state dir legitimately holds generic child names such as
	// certificates/acme, so those names are scoped to the basename check
	// rather than marking arbitrary directories (#1211).
	for _, base := range []string{"veil", "caddy", "mita", "veil-backup.service.d"} {
		dir := filepath.Join(host, "named", base)
		if err := os.MkdirAll(filepath.Join(dir, "certificates", "acme"), 0o755); err != nil {
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

// TestRemovePathRefusesGenericMarkerDirs pins the #1093/#1211 contract: the
// broad generic names the pre-narrowing marker list carried are present in
// countless non-Veil directories, so none of them may bless an
// operator-supplied dir for rm -rf on its own — `--var-dir /home/operator`
// must be refused even though ~/.config exists.
func TestRemovePathRefusesGenericMarkerDirs(t *testing.T) {
	generic := []string{
		"state.json", "generated", "www", "panel", "tls", "certs",
		"backups", "staging", "autocert", ".local", ".config",
		"certificates", "acme",
	}
	for _, marker := range generic {
		t.Run(marker, func(t *testing.T) {
			host := t.TempDir()
			dir := filepath.Join(host, "operator")
			if err := os.MkdirAll(filepath.Join(dir, marker), 0o755); err != nil {
				t.Fatal(err)
			}
			// A second unmarked entry keeps the dir non-empty even where the
			// marker is a file — the refusal must come from the marker check.
			if err := os.WriteFile(filepath.Join(dir, "keep.me"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := ensureRemovablePath(dir); err == nil {
				t.Fatalf("generic marker %q must not bless %s for removal", marker, dir)
			} else if !strings.Contains(err.Error(), "not Veil-managed") {
				t.Fatalf("marker %q: error should explain the refusal: %v", marker, err)
			}
		})
	}
}

// TestRemovePathRefusesFilesystemRoot pins the worst-case #1025 guard:
// `--var-dir /` must never reach os.RemoveAll.
func TestRemovePathRefusesFilesystemRoot(t *testing.T) {
	if err := ensureRemovablePath(string(filepath.Separator)); err == nil {
		t.Fatal("filesystem root must be refused")
	}
}

// TestRemovePathRefusesNonRegularMarkers is the #1242 regression: the marker
// check used os.Stat, which follows a symlink leaf, so an unmarked dir holding
// `veil.db -> <any existing file>` was blessed for os.RemoveAll. A marker name
// only proves ownership when it is a regular file in its own right — symlinks
// (to files or dirs, dangling or not), directories and FIFOs must not bless.
func TestRemovePathRefusesNonRegularMarkers(t *testing.T) {
	host := t.TempDir()
	target := filepath.Join(host, "hosts")
	if err := os.WriteFile(target, []byte("127.0.0.1 localhost\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	targetDir := filepath.Join(host, "elsewhere")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, marker := range stateDirMarkers {
		plants := map[string]func(path string) error{
			"symlink-to-file":  func(path string) error { return os.Symlink(target, path) },
			"symlink-to-dir":   func(path string) error { return os.Symlink(targetDir, path) },
			"symlink-dangling": func(path string) error { return os.Symlink(filepath.Join(host, "missing"), path) },
			"directory":        func(path string) error { return os.Mkdir(path, 0o755) },
		}
		for kind, plant := range plants {
			t.Run(marker+"/"+kind, func(t *testing.T) {
				dir := filepath.Join(t.TempDir(), "operator")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "thesis.txt"), []byte("keep me"), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := plant(filepath.Join(dir, marker)); err != nil {
					t.Skipf("cannot plant %s on this platform: %v", kind, err)
				}
				called := false
				actions := NewActions(nil, func(string) error {
					called = true
					return nil
				})
				err := actions.RemovePath(dir)
				if err == nil {
					t.Fatalf("%s marker %q must not bless %s for removal", kind, marker, dir)
				}
				if !strings.Contains(err.Error(), "not Veil-managed") {
					t.Fatalf("error should explain the refusal: %v", err)
				}
				if called {
					t.Fatal("file remover must not run for a refused path")
				}
			})
		}
	}
	// The symlink target itself is untouched by the refused run.
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("symlink target must survive: %v", err)
	}
}

// TestRemovePathAllowsRegularMarkerBesideDecoy keeps the #1242 fix from
// over-refusing: one regular marker still blesses the tree even when another
// marker name is a symlink.
func TestRemovePathAllowsRegularMarkerBesideDecoy(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "elsewhere"), filepath.Join(dir, "veil.env")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "veil.db"), []byte("sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureRemovablePath(dir); err != nil {
		t.Fatalf("regular veil.db marker must still bless the tree: %v", err)
	}
}
