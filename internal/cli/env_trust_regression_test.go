package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/installer"
)

// TestUninstallRefusesForeignSysctlConfFromEnv is the #1263 CLI regression:
// VEIL_SYSCTL_CONF=/etc/important.conf veil uninstall --yes must not delete
// an arbitrary file as root.
func TestUninstallRefusesForeignSysctlConfFromEnv(t *testing.T) {
	victim := filepath.Join(t.TempDir(), "important.conf")
	if err := os.WriteFile(victim, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VEIL_SYSCTL_CONF", victim)
	oldRemover := uninstallFileRemover
	t.Cleanup(func() { uninstallFileRemover = oldRemover })
	uninstallFileRemover = func(path string) error {
		t.Fatalf("refused uninstall removed %s", path)
		return nil
	}
	cmd := NewRootCommand("test")
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"uninstall", "--dry-run"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "99-veil-quic.conf") {
		t.Fatalf("expected refusal, got %v\n%s", err, out.String())
	}
	if !strings.Contains(errOut.String(), "warning: VEIL_SYSCTL_CONF="+victim) {
		t.Fatalf("env override must be surfaced on stderr, got:\n%s", errOut.String())
	}
	if _, statErr := os.Stat(victim); statErr != nil {
		t.Fatalf("foreign file must survive: %v", statErr)
	}
}

// Env-derived destructive targets are surfaced on stderr so a poisoned root
// environment (sudo -E, exported shell profile) is visible (#1263).
func TestUninstallWarnsOnEnvTargetOverrides(t *testing.T) {
	root := t.TempDir()
	t.Setenv("VEIL_CADDY_STATE_DIR", filepath.Join(root, "caddy"))
	t.Setenv("VEIL_MITA_STATE_DIR", "/var/lib/mita") // packaged default: no warning
	t.Setenv("VEIL_VENDOR_SYSTEMD_DIRS", filepath.Join(root, "vendor"))
	t.Setenv("VEIL_SYSCTL_CONF", filepath.Join(root, "99-veil-quic.conf"))
	cmd := NewRootCommand("test")
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"uninstall", "--dry-run"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("dry-run: %v\n%s", err, errOut.String())
	}
	got := errOut.String()
	for _, want := range []string{"VEIL_CADDY_STATE_DIR=", "VEIL_VENDOR_SYSTEMD_DIRS=", "VEIL_SYSCTL_CONF="} {
		if !strings.Contains(got, "warning: "+want) {
			t.Fatalf("stderr missing warning for %s:\n%s", want, got)
		}
	}
	if strings.Contains(got, "VEIL_MITA_STATE_DIR") {
		t.Fatalf("an override equal to the packaged default must not warn:\n%s", got)
	}
}

// TestUninstallScriptRefusesForeignSysctlConf is the shell-side twin: the
// leftover cleanup rm -f's SYSCTL_CONF as root.
func TestUninstallScriptRefusesForeignSysctlConf(t *testing.T) {
	checkBash(t)
	root := t.TempDir()
	victim := filepath.Join(root, "important.conf")
	if err := os.WriteFile(victim, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "../../scripts/uninstall.sh",
		"--install-dir", filepath.Join(root, "bin"),
		"--etc-dir", filepath.Join(root, "etc"),
		"--var-dir", filepath.Join(root, "var"),
		"--systemd-dir", filepath.Join(root, "systemd"),
		"--yes",
	)
	cmd.Env = append(os.Environ(),
		"VEIL_UNINSTALL_ALLOW_NONROOT=1",
		"VEIL_VENDOR_SYSTEMD_DIRS="+filepath.Join(root, "vendor"),
		"VEIL_SYSCTL_CONF="+victim,
		"VEIL_CADDY_STATE_DIR="+filepath.Join(root, "caddy-state"),
		"VEIL_MITA_STATE_DIR="+filepath.Join(root, "mita-state"),
	)
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "Refusing to use sysctl drop-in") {
		t.Fatalf("expected refusal, got err=%v:\n%s", err, out)
	}
	if _, statErr := os.Stat(victim); statErr != nil {
		t.Fatalf("foreign file must survive: %v\n%s", statErr, out)
	}
}

// VEIL_ACME_INSECURE=0 used to ENABLE insecure ACME (non-empty check) and
// persist =1 into veil.env (#1263). It must follow ParseBool, and an explicit
// process-env value must override the persisted one.
func TestApplyInstallEnvACMEInsecureParsesBool(t *testing.T) {
	for _, tc := range []struct {
		env, persisted string
		want           bool
	}{
		{"0", "", false},
		{"false", "", false},
		{"bogus", "", false},
		{"1", "", true},
		{"true", "", true},
		{"", "1", true},
		{"", "0", false},
		{"0", "1", false},
	} {
		t.Setenv("VEIL_ACME_INSECURE", tc.env)
		profile := installer.RURecommendedProfile{}
		applyInstallEnvToProfile(&profile, ruRecommendedInstallOptions{}, map[string]string{"VEIL_ACME_INSECURE": tc.persisted})
		if profile.ACMEInsecure != tc.want {
			t.Errorf("env=%q persisted=%q: ACMEInsecure = %v, want %v", tc.env, tc.persisted, profile.ACMEInsecure, tc.want)
		}
	}
}
