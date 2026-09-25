package cli

import (
	"os"
	"os/exec"
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
