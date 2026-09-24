package uninstall

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunDryRunPrintsPlanWithoutSideEffects(t *testing.T) {
	var out, errOut bytes.Buffer
	called := false
	err := Run(Options{DryRun: true}, &out, &errOut, Dependencies{
		ServiceStopper:  func(string) error { called = true; return nil },
		FileRemover:     func(string) error { called = true; return nil },
		SystemdReloader: func() error { called = true; return nil },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if called {
		t.Fatal("dry run should not call dependencies")
	}
	// Lock the plan contents: helper socket/service, backup units, warp,
	// protocol units, vendor unit dirs, sysctl drop-in, and the binary must
	// all be listed — a plan that silently drops one would uninstall nothing.
	plan := out.String()
	for _, want := range []string{
		"Veil uninstall plan",
		"veil.service",
		"veil-helper.service",
		"veil-helper.socket",
		"veil-backup.service",
		"veil-backup.timer",
		"veil-warp.service",
		"veil-hysteria2@.service",
		"/lib/systemd/system/veil.service",
		"/usr/lib/systemd/system/veil.service",
		"/etc/sysctl.d/99-veil-quic.conf",
		"/usr/local/bin/veil",
		"Remove configuration and state:",
	} {
		if !strings.Contains(plan, want) {
			t.Fatalf("dry-run plan missing %q:\n%s", want, plan)
		}
	}
}

func TestRunWithConfirmationStopsServicesRemovesPathsAndReloadsSystemd(t *testing.T) {
	var out, errOut bytes.Buffer
	stopped := []string{}
	removed := []string{}
	reloaded := false
	err := Run(Options{Yes: true, EtcDir: "/tmp/etc", VarDir: "/tmp/var", SystemdDir: "/tmp/systemd", InstallDir: "/tmp/bin"}, &out, &errOut, Dependencies{
		ServiceStopper:  func(service string) error { stopped = append(stopped, service); return nil },
		FileRemover:     func(path string) error { removed = append(removed, path); return nil },
		SystemdReloader: func() error { reloaded = true; return nil },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Default uninstall removes configuration and state so a reinstall is fresh.
	if !contains(stopped, "veil-mieru.service") || !contains(removed, "/tmp/systemd/veil-mieru.service") || !contains(removed, "/tmp/bin/veil") || !contains(removed, "/tmp/etc") || !contains(removed, "/tmp/var") || !contains(removed, "/tmp/systemd/veil-backup.service.d") || !contains(removed, "/var/lib/caddy") || !contains(removed, "/var/lib/mita") || !reloaded {
		t.Fatalf("stopped=%+v removed=%+v reloaded=%v", stopped, removed, reloaded)
	}
	if !strings.Contains(out.String(), "Remove configuration and state:") {
		t.Fatalf("output does not report state removal: %s", out.String())
	}
	if !strings.Contains(out.String(), "Uninstalled Veil") {
		t.Fatalf("output = %s", out.String())
	}
}

func TestRunKeepDataPreservesConfigurationAndState(t *testing.T) {
	var out, errOut bytes.Buffer
	removed := []string{}
	err := Run(Options{Yes: true, KeepData: true, EtcDir: "/tmp/etc", VarDir: "/tmp/var", SystemdDir: "/tmp/systemd", InstallDir: "/tmp/bin"}, &out, &errOut, Dependencies{
		ServiceStopper:  func(string) error { return nil },
		FileRemover:     func(path string) error { removed = append(removed, path); return nil },
		SystemdReloader: func() error { return nil },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if contains(removed, "/tmp/etc") || contains(removed, "/tmp/var") || contains(removed, "/var/lib/caddy") || contains(removed, "/var/lib/mita") {
		t.Fatalf("--keep-data must preserve configuration and state, removed=%+v", removed)
	}
	if !contains(removed, "/tmp/bin/veil") || !contains(removed, "/tmp/systemd/veil-backup.service.d") {
		t.Fatalf("--keep-data must still remove the binary and backup drop-in, removed=%+v", removed)
	}
	if !strings.Contains(out.String(), "Preserved state:") {
		t.Fatalf("output does not report preserved state: %s", out.String())
	}
}

func TestRunPurgeOverridesKeepData(t *testing.T) {
	var out, errOut bytes.Buffer
	removed := []string{}
	err := Run(Options{Yes: true, KeepData: true, Purge: true, EtcDir: "/tmp/etc", VarDir: "/tmp/var", SystemdDir: "/tmp/systemd", InstallDir: "/tmp/bin"}, &out, &errOut, Dependencies{
		ServiceStopper:  func(string) error { return nil },
		FileRemover:     func(path string) error { removed = append(removed, path); return nil },
		SystemdReloader: func() error { return nil },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !contains(removed, "/tmp/etc") || !contains(removed, "/tmp/var") {
		t.Fatalf("--purge must override --keep-data and remove state, removed=%+v", removed)
	}
}

func TestRunPurgeRemovesConfigurationAndState(t *testing.T) {
	var out, errOut bytes.Buffer
	var removed []string
	err := Run(Options{
		Yes: true, Purge: true, EtcDir: "/tmp/etc", VarDir: "/tmp/var",
		SystemdDir: "/tmp/systemd", InstallDir: "/tmp/bin",
	}, &out, &errOut, Dependencies{
		ServiceStopper: func(string) error { return nil },
		FileRemover: func(path string) error {
			removed = append(removed, path)
			return nil
		},
		SystemdReloader: func() error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(removed, "/tmp/etc") || !contains(removed, "/tmp/var") {
		t.Fatalf("purge removed=%v", removed)
	}
}

// Issues #492/#486: uninstall must also clear the packaged vendor units under
// /lib + /usr/lib systemd dirs and the QUIC sysctl drop-in — a `dpkg -r` that
// left conffile leftovers (pre-#475 packages) must not survive `veil
// uninstall`.
func TestPathsCoverVendorUnitsAndSysctl(t *testing.T) {
	var out, errOut bytes.Buffer
	removed := []string{}
	err := Run(Options{Yes: true, EtcDir: "/tmp/etc", VarDir: "/tmp/var", SystemdDir: "/tmp/systemd", InstallDir: "/tmp/bin"}, &out, &errOut, Dependencies{
		ServiceStopper:  func(string) error { return nil },
		FileRemover:     func(path string) error { removed = append(removed, path); return nil },
		SystemdReloader: func() error { return nil },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, want := range []string{
		"/lib/systemd/system/veil.service",
		"/lib/systemd/system/veil-backup.service",
		"/usr/lib/systemd/system/veil.service",
		"/usr/lib/systemd/system/veil-helper.socket",
		"/etc/sysctl.d/99-veil-quic.conf",
	} {
		if !contains(removed, want) {
			t.Fatalf("uninstall did not remove %s, removed=%v", want, removed)
		}
	}
	// Keep-data still clears vendor units + sysctl: they are package-owned
	// host files, not operator credentials.
	removed = nil
	out.Reset()
	err = Run(Options{Yes: true, KeepData: true, EtcDir: "/tmp/etc", VarDir: "/tmp/var", SystemdDir: "/tmp/systemd", InstallDir: "/tmp/bin"}, &out, &errOut, Dependencies{
		ServiceStopper:  func(string) error { return nil },
		FileRemover:     func(path string) error { removed = append(removed, path); return nil },
		SystemdReloader: func() error { return nil },
	})
	if err != nil {
		t.Fatalf("Run keep-data: %v", err)
	}
	for _, want := range []string{
		"/lib/systemd/system/veil.service",
		"/etc/sysctl.d/99-veil-quic.conf",
	} {
		if !contains(removed, want) {
			t.Fatalf("--keep-data uninstall did not remove %s, removed=%v", want, removed)
		}
	}
	// The dry-run plan must name the vendor and sysctl paths (issue #501).
	plan := Plan(Options{EtcDir: "/tmp/etc", VarDir: "/tmp/var", SystemdDir: "/tmp/systemd", InstallDir: "/tmp/bin"})
	for _, want := range []string{"/lib/systemd/system/veil.service", "/usr/lib/systemd/system/veil.service", "/etc/sysctl.d/99-veil-quic.conf"} {
		if !strings.Contains(plan, want) {
			t.Fatalf("plan missing %s:\n%s", want, plan)
		}
	}
}

// Issue #375: legacy pre-consolidation veil-caddy@<name>.service instances are
// not in the runtime catalog, so catalog-only scans never see them. Uninstall
// must stop/disable the instance and remove both its enablement wants link and
// a stray per-instance unit file.
func TestRunRemovesLegacyCaddyInstances(t *testing.T) {
	systemdDir := t.TempDir()
	wantsDir := filepath.Join(systemdDir, "multi-user.target.wants")
	if err := os.MkdirAll(wantsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	unitFile := filepath.Join(systemdDir, "veil-caddy@legacy.service")
	if err := os.WriteFile(unitFile, []byte("[Service]\nExecStart=/usr/local/bin/caddy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantLink := filepath.Join(wantsDir, "veil-caddy@legacy.service")
	if err := os.Symlink(unitFile, wantLink); err != nil {
		// Windows test hosts may forbid symlinks; fall back to a regular file —
		// the leftover glob treats both the same.
		if werr := os.WriteFile(wantLink, []byte("x"), 0o644); werr != nil {
			t.Fatalf("plant wants link: %v / %v", err, werr)
		}
	}
	var out, errOut bytes.Buffer
	var stopped, removed []string
	err := Run(Options{
		Yes: true, EtcDir: "/tmp/etc", VarDir: "/tmp/var",
		SystemdDir: systemdDir, InstallDir: "/tmp/bin",
		VendorSystemdDirs: []string{filepath.Join(systemdDir, "vendor")},
	}, &out, &errOut, Dependencies{
		ServiceStopper:  func(service string) error { stopped = append(stopped, service); return nil },
		FileRemover:     func(path string) error { removed = append(removed, path); return nil },
		SystemdReloader: func() error { return nil },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	slashedWant := filepath.ToSlash(wantLink)
	slashedUnit := filepath.ToSlash(unitFile)
	if !contains(stopped, "veil-caddy@legacy.service") {
		t.Fatalf("uninstall did not stop/disable veil-caddy@legacy.service, stopped=%v", stopped)
	}
	if !contains(removed, slashedWant) || !contains(removed, slashedUnit) {
		t.Fatalf("uninstall did not remove legacy caddy want/unit, removed=%v", removed)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
