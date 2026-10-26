package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubRepairDiscovery points the installed-layout discovery at a temp systemd
// tree, so a test can install a veil.service drop-in that records a custom
// EnvironmentFile=.
func stubRepairDiscovery(t *testing.T, systemdDir string) {
	t.Helper()
	original := repairDiscoverySystemdDir
	repairDiscoverySystemdDir = systemdDir
	t.Cleanup(func() { repairDiscoverySystemdDir = original })
	// Host paths must not leak into the discovery.
	t.Setenv("VEIL_ETC_DIR", "")
	t.Setenv("VEIL_VAR_DIR", "")
}

func writeCustomEnvDropIn(t *testing.T, systemdDir, envFile string) {
	t.Helper()
	dir := filepath.Join(systemdDir, "veil.service.d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "10-veil-install.conf"), []byte("[Service]\nEnvironmentFile=-"+envFile+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestRepairRefusesThePackagedDefaultsOnACustomLayout locks #1473: on a host
// installed with a custom --etc-dir/--var-dir, a bare `veil repair` used to
// write veil.env, panel TLS and backups into /etc/veil and /var/lib/veil while
// the custom units kept pointing at the real tree. It now refuses before any
// write and names the flags to use.
func TestRepairRefusesThePackagedDefaultsOnACustomLayout(t *testing.T) {
	systemdDir := t.TempDir()
	stubRepairDiscovery(t, systemdDir)
	customEtc := filepath.Join(t.TempDir(), "opt", "veil", "etc")
	customVar := filepath.Join(t.TempDir(), "opt", "veil", "var")
	envFile := filepath.Join(customEtc, "veil.env")
	if err := os.MkdirAll(customEtc, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "VEIL_KEY_PATH=" + filepath.Join(customEtc, "state.key") + "\nVEIL_STATE_PATH=" + filepath.Join(customVar, "state.json") + "\n"
	if err := os.WriteFile(envFile, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
	writeCustomEnvDropIn(t, systemdDir, envFile)

	cmd := NewRootCommand("test")
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"repair", "--dry-run"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("repair on a custom layout succeeded without flags:\n%s", out.String())
	}
	for _, want := range []string{"custom layout", "--etc-dir " + customEtc, "--var-dir " + customVar} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %v does not mention %q", err, want)
		}
	}
}

// TestRepairWithExplicitFlagsFixesTheCustomLayout keeps the remediation usable:
// with the detected flags repair plans against the custom tree.
func TestRepairWithExplicitFlagsFixesTheCustomLayout(t *testing.T) {
	systemdDir := t.TempDir()
	stubRepairDiscovery(t, systemdDir)
	customEtc := filepath.Join(t.TempDir(), "opt", "veil", "etc")
	customVar := filepath.Join(t.TempDir(), "opt", "veil", "var")
	envFile := filepath.Join(customEtc, "veil.env")
	if err := os.MkdirAll(customEtc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(envFile, []byte("VEIL_KEY_PATH="+filepath.Join(customEtc, "state.key")+"\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	writeCustomEnvDropIn(t, systemdDir, envFile)

	cmd := NewRootCommand("test")
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{
		"repair", "--dry-run",
		"--etc-dir", customEtc,
		"--var-dir", customVar,
		"--systemd-dir", systemdDir,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("repair with explicit custom paths: %v\n%s", err, out.String())
	}
	text := out.String()
	if strings.Contains(text, "/etc/veil") && !strings.Contains(text, customEtc) {
		t.Fatalf("repair planned the packaged tree on a custom install:\n%s", text)
	}
}

// TestRepairKeepsThePackagedDefaultsWithoutAnInstalledLayout keeps dev and
// container hosts working: discovery finds no environment file, so repair runs
// against the defaults as before.
func TestRepairKeepsThePackagedDefaultsWithoutAnInstalledLayout(t *testing.T) {
	stubRepairDiscovery(t, t.TempDir())
	cmd := NewRootCommand("test")
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"repair", "--dry-run"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("repair without an installed layout: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "custom layout") {
		t.Fatalf("default layout reported as custom:\n%s", out.String())
	}
}

// TestRepairUsesTheProcessEnvironmentLayout locks the inert-env branch: with
// VEIL_ETC_DIR/VEIL_VAR_DIR exported, repair has to plan against those trees
// instead of the hardcoded flag defaults (the branch used to return the
// defaults, so the plan silently targeted /etc/veil).
func TestRepairUsesTheProcessEnvironmentLayout(t *testing.T) {
	stubRepairDiscovery(t, t.TempDir())
	customEtc := filepath.Join(t.TempDir(), "opt", "veil", "etc")
	customVar := filepath.Join(t.TempDir(), "opt", "veil", "var")
	t.Setenv("VEIL_ETC_DIR", customEtc)
	t.Setenv("VEIL_VAR_DIR", customVar)

	cmd := NewRootCommand("test")
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"repair", "--dry-run"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("repair with an explicit process layout: %v\n%s", err, out.String())
	}
	text := out.String()
	if !strings.Contains(text, customEtc) {
		t.Fatalf("repair did not use VEIL_ETC_DIR:\n%s", text)
	}
	if strings.Contains(text, "/etc/veil") && !strings.Contains(text, customEtc) {
		t.Fatalf("repair planned the packaged tree although VEIL_ETC_DIR is set:\n%s", text)
	}
}

// TestRepairRefusesAPartiallyRecordedCustomLayout locks the second fallback:
// a repaired env may carry only the etc block (VEIL_VAR_DIR is absent), which
// used to make discovery report "not installed" and a bare repair write to
// /etc/veil again. Each recorded dimension has to be compared on its own.
func TestRepairRefusesAPartiallyRecordedCustomLayout(t *testing.T) {
	systemdDir := t.TempDir()
	stubRepairDiscovery(t, systemdDir)
	customEtc := filepath.Join(t.TempDir(), "opt", "veil", "etc")
	customVar := filepath.Join(t.TempDir(), "opt", "veil", "var")
	envFile := filepath.Join(customEtc, "veil.env")
	if err := os.MkdirAll(customEtc, 0o755); err != nil {
		t.Fatal(err)
	}
	// Etc block only: exactly what `repair` regenerates before this fix.
	body := "VEIL_ETC_DIR=" + customEtc + "\nVEIL_KEY_PATH=" + filepath.Join(customEtc, "state.key") + "\n"
	if err := os.WriteFile(envFile, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
	writeCustomEnvDropIn(t, systemdDir, envFile)

	cmd := NewRootCommand("test")
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"repair", "--dry-run"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("repair on a partially recorded custom layout fell back to the defaults:\n%s", out.String())
	}
	if !strings.Contains(err.Error(), "custom layout") || !strings.Contains(err.Error(), customEtc) {
		t.Fatalf("error %v does not name the custom etc dir", err)
	}
	// The unknown dimension is offered with the packaged default.
	if !strings.Contains(err.Error(), "--var-dir /var/lib/veil") {
		t.Fatalf("error %v does not suggest the var dir to pass", err)
	}
	_ = customVar
}
