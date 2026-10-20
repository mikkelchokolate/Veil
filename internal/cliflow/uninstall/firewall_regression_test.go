package uninstall

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func firewallTestOptions(t *testing.T) Options {
	return Options{
		Yes: true, EtcDir: t.TempDir(), VarDir: t.TempDir(),
		SystemdDir: t.TempDir(), InstallDir: t.TempDir(),
		CaddyStateDir: t.TempDir(), MitaStateDir: t.TempDir(),
		VendorSystemdDirs: []string{t.TempDir()},
		SysctlConfPath:    t.TempDir() + "/99-veil-quic.conf",
	}
}

func noopDeps(cleaner func() ([]string, error)) Dependencies {
	return Dependencies{
		ServiceStopper:  func(string) error { return nil },
		FileRemover:     func(string) error { return nil },
		SystemdReloader: func() error { return nil },
		FirewallCleaner: cleaner,
	}
}

// TestRunRemovesVeilFirewallRules is the #1289 regression: a full uninstall
// must close the UFW allows Veil opened and say which ones it closed.
func TestRunRemovesVeilFirewallRules(t *testing.T) {
	var out, errOut bytes.Buffer
	calls := 0
	err := Run(firewallTestOptions(t), &out, &errOut, noopDeps(func() ([]string, error) {
		calls++
		return []string{"2096/tcp", "80/tcp"}, nil
	}))
	if err != nil {
		t.Fatalf("Run: %v\n%s", err, errOut.String())
	}
	if calls != 1 {
		t.Fatalf("firewall cleaner calls = %d, want 1", calls)
	}
	for _, want := range []string{"Remove firewall rules:", "Veil management SSH", "Removed Veil firewall rule 2096/tcp", "Removed Veil firewall rule 80/tcp", "Uninstalled Veil"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
}

// --keep-data preserves the configuration a reinstall reuses, so the ports
// it serves must stay open too.
func TestRunKeepDataPreservesFirewallRules(t *testing.T) {
	opts := firewallTestOptions(t)
	opts.KeepData = true
	var out, errOut bytes.Buffer
	err := Run(opts, &out, &errOut, noopDeps(func() ([]string, error) {
		t.Fatal("--keep-data must not touch the firewall")
		return nil, nil
	}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.String(), "Preserved firewall rules:") {
		t.Fatalf("plan must say the firewall is kept:\n%s", out.String())
	}
}

// --purge wins over --keep-data for the firewall exactly as for state dirs.
func TestRunPurgeOverridesKeepDataForFirewall(t *testing.T) {
	opts := firewallTestOptions(t)
	opts.KeepData, opts.Purge = true, true
	calls := 0
	var out, errOut bytes.Buffer
	if err := Run(opts, &out, &errOut, noopDeps(func() ([]string, error) { calls++; return nil, nil })); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if calls != 1 {
		t.Fatalf("--purge must clean the firewall, calls = %d", calls)
	}
}

// A firewall cleanup failure leaves ports open, so the uninstall must fail
// closed like every other step (#1026) while still reporting what it closed.
func TestRunFirewallFailureFailsClosed(t *testing.T) {
	var out, errOut bytes.Buffer
	removedPaths := 0
	deps := noopDeps(func() ([]string, error) {
		return []string{"80/tcp"}, errors.New("delete 2096/tcp: exit status 1")
	})
	deps.FileRemover = func(string) error { removedPaths++; return nil }
	err := Run(firewallTestOptions(t), &out, &errOut, deps)
	if err == nil {
		t.Fatal("uninstall must fail closed when firewall rules could not be removed")
	}
	if !strings.Contains(errOut.String(), "warning: firewall: delete 2096/tcp") {
		t.Fatalf("errOut = %s", errOut.String())
	}
	if !strings.Contains(out.String(), "Removed Veil firewall rule 80/tcp") {
		t.Fatalf("partial progress must still be reported:\n%s", out.String())
	}
	if strings.Contains(out.String(), "Uninstalled Veil") {
		t.Fatalf("incomplete uninstall must not claim success:\n%s", out.String())
	}
	if removedPaths == 0 {
		t.Fatal("a firewall failure must not stop the remaining cleanup")
	}
}

func TestRunDryRunDoesNotTouchFirewall(t *testing.T) {
	opts := firewallTestOptions(t)
	opts.DryRun = true
	var out, errOut bytes.Buffer
	if err := Run(opts, &out, &errOut, noopDeps(func() ([]string, error) {
		t.Fatal("--dry-run must not touch the firewall")
		return nil, nil
	})); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.String(), "Remove firewall rules:") {
		t.Fatalf("dry-run plan must list the firewall step:\n%s", out.String())
	}
}

func TestDefaultDependenciesWireFirewallCleaner(t *testing.T) {
	if DefaultDependencies().FirewallCleaner == nil {
		t.Fatal("default dependencies must clean Veil firewall rules (#1289)")
	}
}
