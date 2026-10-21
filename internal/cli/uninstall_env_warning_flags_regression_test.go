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
	var errOut bytes.Buffer
	warnUninstallEnvOverrides(&errOut, nil)
	if errOut.Len() != 0 {
		t.Fatalf("cleaned-equal packaged paths reported as overrides:\n%s", errOut.String())
	}
	t.Setenv("VEIL_SYSCTL_CONF", "/tmp/elsewhere/99-veil-quic.conf")
	warnUninstallEnvOverrides(&errOut, nil)
	if !strings.Contains(errOut.String(), "VEIL_SYSCTL_CONF=/tmp/elsewhere/99-veil-quic.conf") {
		t.Fatalf("real override not reported:\n%s", errOut.String())
	}
}
