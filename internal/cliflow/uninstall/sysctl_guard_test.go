package uninstall

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunRefusesForeignSysctlConfTarget is the #1263 regression: the sysctl
// drop-in path comes from VEIL_SYSCTL_CONF and is removed unconditionally as
// root, so a path that is not Veil's 99-veil-quic.conf must fail closed
// before ANY step runs (no service stops, no removals).
func TestRunRefusesForeignSysctlConfTarget(t *testing.T) {
	victim := filepath.Join(t.TempDir(), "important.conf")
	if err := os.WriteFile(victim, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, dryRun := range []bool{false, true} {
		opts := firewallTestOptions(t)
		opts.SysctlConfPath = victim
		opts.DryRun = dryRun
		var removed []string
		stopped := 0
		deps := noopDeps(nil)
		deps.FileRemover = func(path string) error { removed = append(removed, path); return nil }
		deps.ServiceStopper = func(string) error { stopped++; return nil }
		var out, errOut bytes.Buffer
		err := Run(opts, &out, &errOut, deps)
		if err == nil || !strings.Contains(err.Error(), "99-veil-quic.conf") {
			t.Fatalf("dryRun=%v: Run error = %v, want refusal naming 99-veil-quic.conf", dryRun, err)
		}
		if len(removed) != 0 || stopped != 0 {
			t.Fatalf("dryRun=%v: refused run must not act: removed=%v stopped=%d", dryRun, removed, stopped)
		}
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("foreign file must survive: %v", err)
	}
}

func TestValidateSysctlConfPath(t *testing.T) {
	for path, ok := range map[string]bool{
		"/etc/sysctl.d/99-veil-quic.conf":     true,
		"/tmp/alt/99-veil-quic.conf":          true,
		"/etc/sysctl.d/99-veil-quic.conf/":    true,
		"/etc/important.conf":                 false,
		"/etc/sysctl.d":                       false,
		"/":                                   false,
		"/etc/sysctl.d/99-veil-quic.conf.bak": false,
	} {
		if err := ValidateSysctlConfPath(path); (err == nil) != ok {
			t.Errorf("ValidateSysctlConfPath(%q) err = %v, want ok=%v", path, err, ok)
		}
	}
}
