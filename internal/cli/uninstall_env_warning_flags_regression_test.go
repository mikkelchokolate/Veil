package cli

import (
	"bytes"
	"strings"
	"testing"
)

// #1334 review: the env-override warning must describe the target the run
// actually uses. An explicit --caddy-state-dir replaces VEIL_CADDY_STATE_DIR,
// so warning that the env retargets removal would be false; without the flag
// the env value is used and the warning must still fire.
func TestUninstallEnvWarningRespectsExplicitFlags(t *testing.T) {
	t.Setenv("VEIL_CADDY_STATE_DIR", "/tmp/veil-1334-elsewhere")
	t.Setenv("VEIL_MITA_STATE_DIR", "/tmp/veil-1334-mita-elsewhere")
	run := func(args ...string) string {
		cmd := NewRootCommand("test")
		var out, errOut bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)
		cmd.SetArgs(append([]string{"uninstall", "--dry-run"}, args...))
		if err := cmd.Execute(); err != nil {
			t.Fatalf("uninstall %v: %v\n%s%s", args, err, out.String(), errOut.String())
		}
		return errOut.String()
	}

	withFlag := run("--caddy-state-dir", "/var/lib/caddy")
	if strings.Contains(withFlag, "VEIL_CADDY_STATE_DIR") {
		t.Fatalf("warned about VEIL_CADDY_STATE_DIR although --caddy-state-dir overrode it:\n%s", withFlag)
	}
	if !strings.Contains(withFlag, "VEIL_MITA_STATE_DIR=/tmp/veil-1334-mita-elsewhere") {
		t.Fatalf("VEIL_MITA_STATE_DIR is still in effect and must be warned about:\n%s", withFlag)
	}

	withMitaFlag := run("--mita-state-dir", "/var/lib/mita")
	if strings.Contains(withMitaFlag, "VEIL_MITA_STATE_DIR") {
		t.Fatalf("warned about VEIL_MITA_STATE_DIR although --mita-state-dir overrode it:\n%s", withMitaFlag)
	}

	withoutFlag := run()
	if !strings.Contains(withoutFlag, "VEIL_CADDY_STATE_DIR=/tmp/veil-1334-elsewhere") {
		t.Fatalf("env override in effect but not warned about:\n%s", withoutFlag)
	}
}

// #1334 review: a cleaned-equal path (trailing slash) is the packaged target
// and must not be reported as an override.
func TestUninstallEnvWarningComparesCleanedPaths(t *testing.T) {
	t.Setenv("VEIL_SYSCTL_CONF", "/etc/sysctl.d/99-veil-quic.conf/")
	t.Setenv("VEIL_CADDY_STATE_DIR", "/var/lib/caddy/")
	// Neutralise the other overrides so a dev shell or runner exporting them
	// cannot fail this test for unrelated reasons (empty is skipped).
	t.Setenv("VEIL_MITA_STATE_DIR", "")
	t.Setenv("VEIL_VENDOR_SYSTEMD_DIRS", "")
	var errOut bytes.Buffer
	warnUninstallEnvOverrides(&errOut, nil)
	if errOut.Len() != 0 {
		t.Fatalf("cleaned-equal packaged paths reported as overrides:\n%s", errOut.String())
	}
	// The flow consumes the vendor list via strings.Fields and single paths
	// via strings.TrimSpace, so padded spellings are still the packaged
	// targets and must not warn (#1342 review).
	t.Setenv("VEIL_VENDOR_SYSTEMD_DIRS", "  /lib/systemd/system \t /usr/lib/systemd/system ")
	t.Setenv("VEIL_CADDY_STATE_DIR", " /var/lib/caddy ")
	warnUninstallEnvOverrides(&errOut, nil)
	if errOut.Len() != 0 {
		t.Fatalf("whitespace-padded packaged targets reported as overrides:\n%s", errOut.String())
	}
	t.Setenv("VEIL_SYSCTL_CONF", "/tmp/elsewhere/99-veil-quic.conf")
	warnUninstallEnvOverrides(&errOut, nil)
	if !strings.Contains(errOut.String(), "VEIL_SYSCTL_CONF=/tmp/elsewhere/99-veil-quic.conf") {
		t.Fatalf("real override not reported:\n%s", errOut.String())
	}
}

// Every override's flag mapping must name a real uninstall flag: pflag's
// Changed returns false for an unknown name, so a typo or a later rename
// would silently bring the false warning back.
func TestUninstallEnvOverrideFlagsExist(t *testing.T) {
	flags := newUninstallCommand().Flags()
	for _, override := range uninstallEnvOverrides {
		if override.flag == "" {
			continue
		}
		if flags.Lookup(override.flag) == nil {
			t.Errorf("%s maps to unknown uninstall flag --%s", override.name, override.flag)
		}
	}
}
