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
