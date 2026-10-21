package cli

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

// TestUninstallScriptRefusesUnmarkedStateDir is the #1025 shell-side
// regression: the leftover-state cleanup feeds --var-dir/--etc-dir (and the
// VEIL_*_STATE_DIR env overrides) straight into rm -rf, so a directory that
// is not Veil-managed must fail closed instead of being wiped.
func TestUninstallScriptRefusesUnmarkedStateDir(t *testing.T) {
	checkBash(t)
	root := t.TempDir()
	installDir := filepath.Join(root, "bin")
	etcDir := filepath.Join(root, "etc", "veil")
	varDir := filepath.Join(root, "photos") // hostile name AND unmarked
	systemdDir := filepath.Join(root, "systemd")

	for _, dir := range []string{installDir, etcDir, varDir, systemdDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// etcDir is marked Veil-owned; varDir holds unrelated data only.
	if err := os.WriteFile(filepath.Join(etcDir, "veil.env"), []byte("VEIL_API_TOKEN=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(varDir, "vacation.jpg"), []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A unit leftover makes has_leftover_state true so the cleanup runs.
	if err := os.WriteFile(filepath.Join(systemdDir, "veil.service"), []byte("[Service]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", "../../scripts/uninstall.sh",
		"--install-dir", installDir,
		"--etc-dir", etcDir,
		"--var-dir", varDir,
		"--systemd-dir", systemdDir,
		"--yes",
	)
	cmd.Env = append(os.Environ(),
		"VEIL_UNINSTALL_ALLOW_NONROOT=1",
		"VEIL_VENDOR_SYSTEMD_DIRS="+filepath.Join(root, "vendor"),
		"VEIL_SYSCTL_CONF="+filepath.Join(root, "sysctl.conf"),
		"VEIL_CADDY_STATE_DIR="+filepath.Join(root, "caddy-state"),
		"VEIL_MITA_STATE_DIR="+filepath.Join(root, "mita-state"),
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("uninstall must fail closed on an unmarked state dir:\n%s", out)
	}
	if !strings.Contains(string(out), "Refusing to remove") {
		t.Fatalf("expected a refusal explaining the guard, got:\n%s", out)
	}
	if _, statErr := os.Stat(filepath.Join(varDir, "vacation.jpg")); statErr != nil {
		t.Fatalf("unmarked directory content must survive: %v\n%s", statErr, out)
	}
	// The marked etcDir must still have been... no: the guard runs before ANY
	// rm -rf so even marked dirs survive a refused run — fail closed means no
	// partial wipe either.
	if _, statErr := os.Stat(filepath.Join(etcDir, "veil.env")); statErr != nil {
		t.Fatalf("guard must run before any removal; marked dir should survive the refused run: %v", statErr)
	}
}

// TestUninstallScriptRemovesMarkedStateDir proves the #1025 guard does not
// block legitimate cleanup: Veil-marked state dirs are still removed.
func TestUninstallScriptRemovesMarkedStateDir(t *testing.T) {
	checkBash(t)
	root := t.TempDir()
	installDir := filepath.Join(root, "bin")
	etcDir := filepath.Join(root, "etc", "veil")
	varDir := filepath.Join(root, "var", "state") // unmarked name, marked content
	systemdDir := filepath.Join(root, "systemd")

	for _, dir := range []string{installDir, etcDir, varDir, systemdDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(etcDir, "veil.env"), []byte("VEIL_API_TOKEN=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(varDir, "veil.db"), []byte("sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(varDir, "state.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(systemdDir, "veil.service"), []byte("[Service]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", "../../scripts/uninstall.sh",
		"--install-dir", installDir,
		"--etc-dir", etcDir,
		"--var-dir", varDir,
		"--systemd-dir", systemdDir,
		"--yes",
	)
	cmd.Env = append(os.Environ(),
		"VEIL_UNINSTALL_ALLOW_NONROOT=1",
		"VEIL_VENDOR_SYSTEMD_DIRS="+filepath.Join(root, "vendor"),
		"VEIL_SYSCTL_CONF="+filepath.Join(root, "sysctl.conf"),
		"VEIL_CADDY_STATE_DIR="+filepath.Join(root, "caddy-state"),
		"VEIL_MITA_STATE_DIR="+filepath.Join(root, "mita-state"),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("marked-state uninstall should succeed: %v\n%s", err, out)
	}
	for _, dir := range []string{etcDir, varDir} {
		if _, statErr := os.Lstat(dir); !os.IsNotExist(statErr) {
			t.Fatalf("marked state dir %s should be removed:\n%s", dir, out)
		}
	}
}

// TestUninstallScriptRefusesGenericallyMarkedDir is the #1093 regression: the
// pre-fix marker list blessed a directory for rm -rf on a single generic child
// (.config, www, backups, ...), so VEIL_ETC_DIR=/home/operator — a dir that
// almost always holds .config — passed the guard. Only distinctive
// Veil-written names may authorize removal.
func TestUninstallScriptRefusesGenericallyMarkedDir(t *testing.T) {
	checkBash(t)
	root := t.TempDir()
	installDir := filepath.Join(root, "bin")
	etcDir := filepath.Join(root, "etc", "veil")
	// A "home-like" state dir: generic .config content but nothing Veil wrote.
	varDir := filepath.Join(root, "home-operator")
	systemdDir := filepath.Join(root, "systemd")

	for _, dir := range []string{installDir, etcDir, varDir, systemdDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(etcDir, "veil.env"), []byte("VEIL_API_TOKEN=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, generic := range []string{".config", ".local", "www", "backups", "staging", "tls", "panel", "generated", "certs", "certificates", "acme", "autocert"} {
		if err := os.MkdirAll(filepath.Join(varDir, generic), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// state.json is generic too — plenty of projects ship one.
	if err := os.WriteFile(filepath.Join(varDir, "state.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(systemdDir, "veil.service"), []byte("[Service]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", "../../scripts/uninstall.sh",
		"--install-dir", installDir,
		"--etc-dir", etcDir,
		"--var-dir", varDir,
		"--systemd-dir", systemdDir,
		"--yes",
	)
	cmd.Env = append(os.Environ(),
		"VEIL_UNINSTALL_ALLOW_NONROOT=1",
		"VEIL_VENDOR_SYSTEMD_DIRS="+filepath.Join(root, "vendor"),
		"VEIL_SYSCTL_CONF="+filepath.Join(root, "sysctl.conf"),
		"VEIL_CADDY_STATE_DIR="+filepath.Join(root, "caddy-state"),
		"VEIL_MITA_STATE_DIR="+filepath.Join(root, "mita-state"),
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("uninstall must refuse a dir marked only with generic names:\n%s", out)
	}
	if !strings.Contains(string(out), "Refusing to remove") {
		t.Fatalf("expected a refusal explaining the guard, got:\n%s", out)
	}
	if _, statErr := os.Stat(filepath.Join(varDir, ".config")); statErr != nil {
		t.Fatalf("generic-marked dir content must survive: %v\n%s", statErr, out)
	}
}

// TestUninstallScriptRefusesSymlinkOrDirMarker is the #1242 shell-side
// regression: is_safe_state_dir used `[ -e "$dir/$marker" ]`, which follows a
// symlink leaf, so an unmarked dir holding `veil.db -> <existing file>` passed
// the guard and reached rm -rf. Markers must be regular non-symlink files —
// a symlinked or directory-shaped marker name proves nothing.
func TestUninstallScriptRefusesSymlinkOrDirMarker(t *testing.T) {
	checkBash(t)
	plants := map[string]func(t *testing.T, root, varDir string){
		"symlink-to-file": func(t *testing.T, root, varDir string) {
			target := filepath.Join(root, "hosts")
			if err := os.WriteFile(target, []byte("127.0.0.1 localhost\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(varDir, "veil.db")); err != nil {
				t.Skipf("symlinks unsupported: %v", err)
			}
		},
		"symlink-to-dir": func(t *testing.T, root, varDir string) {
			if err := os.Symlink(root, filepath.Join(varDir, "state.key")); err != nil {
				t.Skipf("symlinks unsupported: %v", err)
			}
		},
		"directory": func(t *testing.T, root, varDir string) {
			if err := os.Mkdir(filepath.Join(varDir, ".veil-managed"), 0o755); err != nil {
				t.Fatal(err)
			}
		},
	}
	for kind, plant := range plants {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			installDir := filepath.Join(root, "bin")
			etcDir := filepath.Join(root, "etc", "veil")
			varDir := filepath.Join(root, "documents")
			systemdDir := filepath.Join(root, "systemd")
			for _, dir := range []string{installDir, etcDir, varDir, systemdDir} {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(etcDir, "veil.env"), []byte("VEIL_API_TOKEN=x\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(varDir, "thesis.txt"), []byte("keep me"), 0o644); err != nil {
				t.Fatal(err)
			}
			plant(t, root, varDir)
			if err := os.WriteFile(filepath.Join(systemdDir, "veil.service"), []byte("[Service]\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			cmd := exec.Command("bash", "../../scripts/uninstall.sh",
				"--install-dir", installDir,
				"--etc-dir", etcDir,
				"--var-dir", varDir,
				"--systemd-dir", systemdDir,
				"--yes",
			)
			cmd.Env = append(os.Environ(),
				"VEIL_UNINSTALL_ALLOW_NONROOT=1",
				"VEIL_VENDOR_SYSTEMD_DIRS="+filepath.Join(root, "vendor"),
				"VEIL_SYSCTL_CONF="+filepath.Join(root, "sysctl.conf"),
				"VEIL_CADDY_STATE_DIR="+filepath.Join(root, "caddy-state"),
				"VEIL_MITA_STATE_DIR="+filepath.Join(root, "mita-state"),
			)
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("uninstall must refuse a dir whose only marker is a %s:\n%s", kind, out)
			}
			if !strings.Contains(string(out), "Refusing to remove") {
				t.Fatalf("expected a refusal explaining the guard, got:\n%s", out)
			}
			if _, statErr := os.Stat(filepath.Join(varDir, "thesis.txt")); statErr != nil {
				t.Fatalf("unmarked directory content must survive: %v\n%s", statErr, out)
			}
		})
	}
}

// TestUninstallScriptRequiresProofForSharedCaddyDir is the #1244 shell-side
// regression: /var/lib/caddy is also the stock system Caddy data dir, so the
// `caddy` basename alone must not bless it for rm -rf. A caddy dir owned by a
// non-Veil account fails closed; a Veil-marked one is still removed.
func TestUninstallScriptRequiresProofForSharedCaddyDir(t *testing.T) {
	checkBash(t)
	for _, name := range []string{"veil", "veil-proxy", "veil-mita"} {
		if u, err := user.Current(); err == nil && u.Username == name {
			t.Skipf("test runs as Veil service account %s", name)
		}
	}
	for _, marked := range []bool{false, true} {
		root := t.TempDir()
		installDir := filepath.Join(root, "bin")
		etcDir := filepath.Join(root, "etc", "veil")
		varDir := filepath.Join(root, "var", "lib", "veil")
		caddyDir := filepath.Join(root, "var", "lib", "caddy")
		systemdDir := filepath.Join(root, "systemd")
		for _, dir := range []string{installDir, etcDir, varDir, filepath.Join(caddyDir, "caddy", "certificates"), systemdDir} {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		stockKey := filepath.Join(caddyDir, "caddy", "certificates", "site.key")
		if err := os.WriteFile(stockKey, []byte("stock caddy cert"), 0o600); err != nil {
			t.Fatal(err)
		}
		if marked {
			if err := os.WriteFile(filepath.Join(caddyDir, ".veil-managed"), []byte("veil\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(systemdDir, "veil.service"), []byte("[Service]\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("bash", "../../scripts/uninstall.sh",
			"--install-dir", installDir,
			"--etc-dir", etcDir,
			"--var-dir", varDir,
			"--systemd-dir", systemdDir,
			"--yes",
		)
		cmd.Env = append(os.Environ(),
			"VEIL_UNINSTALL_ALLOW_NONROOT=1",
			"VEIL_VENDOR_SYSTEMD_DIRS="+filepath.Join(root, "vendor"),
			"VEIL_SYSCTL_CONF="+filepath.Join(root, "sysctl.conf"),
			"VEIL_CADDY_STATE_DIR="+caddyDir,
			"VEIL_MITA_STATE_DIR="+filepath.Join(root, "var", "lib", "mita"),
		)
		out, err := cmd.CombinedOutput()
		if !marked {
			if err == nil || !strings.Contains(string(out), "Refusing to remove "+caddyDir) {
				t.Fatalf("unowned, unmarked caddy dir must fail closed, err=%v:\n%s", err, out)
			}
			if _, statErr := os.Stat(stockKey); statErr != nil {
				t.Fatalf("stock caddy data must survive: %v\n%s", statErr, out)
			}
			continue
		}
		if err != nil {
			t.Fatalf("marked caddy dir should be removed: %v\n%s", err, out)
		}
		if _, statErr := os.Lstat(caddyDir); !os.IsNotExist(statErr) {
			t.Fatalf("marked caddy dir %s should be removed:\n%s", caddyDir, out)
		}
	}
}
