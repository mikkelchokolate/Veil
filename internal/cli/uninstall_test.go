package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	uninstallflow "github.com/mikkelchokolate/Veil/internal/cliflow/uninstall"
)

func TestUninstallRefusesWithoutYes(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"uninstall"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error without --yes")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("expected --yes message, got: %v", err)
	}
}

func TestUninstallDryRunShowsPlan(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"uninstall", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"Veil uninstall plan",
		"Stop services:",
		"Disable services:",
		"Remove configuration and state:",
		"Remove systemd units:",
		"/etc/systemd/system/veil.service",
		"/etc/systemd/system/veil-olcrtc@.service",
		"/etc/systemd/system/veil-mieru.service",
		"/etc/systemd/system/veil-backup.service.d",
		"/var/lib/caddy",
		"/var/lib/mita",
		"Remove binary:",
	} {
		if !strings.Contains(filepath.ToSlash(got), filepath.ToSlash(want)) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestUninstallDryRunHonorsCustomPaths(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"uninstall", "--dry-run", "--etc-dir", "/tmp/veil-etc", "--var-dir", "/tmp/veil-var", "--systemd-dir", "/tmp/systemd", "--install-dir", "/opt/veil/bin"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{"/tmp/veil-etc", "/tmp/veil-var", "/tmp/systemd/veil.service", "/tmp/systemd/veil-mieru.service", "/opt/veil/bin/veil"} {
		if !strings.Contains(filepath.ToSlash(got), filepath.ToSlash(want)) {
			t.Fatalf("custom uninstall plan missing %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"/etc/veil", "/var/lib/veil", "/etc/systemd/system/veil.service", "/usr/local/bin/veil"} {
		if strings.Contains(filepath.ToSlash(got), filepath.ToSlash(unwanted)) {
			t.Fatalf("custom uninstall plan should not include default %q:\n%s", unwanted, got)
		}
	}
}

func TestUninstallYesExecutesUninstall(t *testing.T) {
	origStop := uninstallServiceStopper
	origRemove := uninstallFileRemover
	origReload := uninstallSystemdReloader
	t.Cleanup(func() {
		uninstallServiceStopper = origStop
		uninstallFileRemover = origRemove
		uninstallSystemdReloader = origReload
	})

	stopped := []string{}
	removed := []string{}
	reloaded := false

	uninstallServiceStopper = func(service string) error {
		stopped = append(stopped, service)
		return nil
	}
	uninstallFileRemover = func(path string) error {
		removed = append(removed, path)
		return nil
	}
	uninstallSystemdReloader = func() error {
		reloaded = true
		return nil
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"uninstall", "--yes"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}

	// Verify services are stopped
	for _, svc := range []string{"veil.service", "veil-caddy.service", "veil-hysteria2@.service", "veil-olcrtc@.service", "veil-warp.service", "veil-mieru.service"} {
		found := false
		for _, s := range stopped {
			if s == svc {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("service %s was not stopped", svc)
		}
	}

	// Verify units, binary, configuration, and state are all removed by default.
	for _, path := range []string{"/etc/systemd/system/veil.service", "/etc/systemd/system/veil-olcrtc@.service", "/etc/systemd/system/veil-mieru.service", "/etc/systemd/system/veil-backup.service.d", "/usr/local/bin/veil", "/etc/veil", "/var/lib/veil", "/var/lib/caddy", "/var/lib/mita"} {
		found := false
		for _, r := range removed {
			if filepath.ToSlash(r) == filepath.ToSlash(path) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("path %s was not removed", path)
		}
	}

	if !reloaded {
		t.Fatal("systemd daemon-reload was not run after removing unit files")
	}

	// Verify output
	got := out.String()
	for _, want := range []string{
		"Uninstalled Veil",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestUninstallYesRemovesBackupDropInAndTemplateInstanceWants(t *testing.T) {
	origStop := uninstallServiceStopper
	origRemove := uninstallFileRemover
	origReload := uninstallSystemdReloader
	t.Cleanup(func() {
		uninstallServiceStopper = origStop
		uninstallFileRemover = origRemove
		uninstallSystemdReloader = origReload
	})
	host := t.TempDir()
	systemdDir := filepath.Join(host, "systemd")
	wantsDir := filepath.Join(systemdDir, "multi-user.target.wants")
	if err := os.MkdirAll(wantsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hy2Want := filepath.Join(wantsDir, "veil-hysteria2@hy2-main.service")
	olcWant := filepath.Join(wantsDir, "veil-olcrtc@room-1.service")
	if err := os.WriteFile(hy2Want, []byte("want"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(olcWant, []byte("want"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stopped, removed []string
	uninstallServiceStopper = func(service string) error { stopped = append(stopped, service); return nil }
	uninstallFileRemover = func(path string) error {
		removed = append(removed, filepath.ToSlash(path))
		return nil
	}
	uninstallSystemdReloader = func() error { return nil }
	cmd := NewRootCommand("test")
	cmd.SetArgs([]string{
		"uninstall", "--yes",
		"--etc-dir", filepath.Join(host, "etc"),
		"--var-dir", filepath.Join(host, "var"),
		"--systemd-dir", systemdDir,
		"--install-dir", filepath.Join(host, "bin"),
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"veil-hysteria2@hy2-main.service", "veil-olcrtc@room-1.service"} {
		if !contains(stopped, want) {
			t.Fatalf("did not disable leftover instance %s, stopped=%v", want, stopped)
		}
	}
	dropIn := filepath.ToSlash(filepath.Join(systemdDir, "veil-backup.service.d"))
	if !contains(removed, dropIn) || !contains(removed, filepath.ToSlash(hy2Want)) || !contains(removed, filepath.ToSlash(olcWant)) {
		t.Fatalf("did not remove leftover systemd paths, removed=%v", removed)
	}
}

func TestUninstallRegisteredInRootCommand(t *testing.T) {
	cmd := NewRootCommand("test")
	found := false
	for _, sub := range cmd.Commands() {
		if sub.Name() == "uninstall" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("uninstall command not registered in root command")
	}
}

func TestUninstallHasCorrectHelp(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"uninstall", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "uninstall") || !strings.Contains(got, "--yes") || !strings.Contains(got, "--dry-run") || !strings.Contains(got, "--purge") || !strings.Contains(got, "--keep-data") {
		t.Fatalf("help output missing expected content:\n%s", got)
	}
}

func TestUninstallKeepDataPreservesState(t *testing.T) {
	origStop := uninstallServiceStopper
	origRemove := uninstallFileRemover
	origReload := uninstallSystemdReloader
	t.Cleanup(func() {
		uninstallServiceStopper = origStop
		uninstallFileRemover = origRemove
		uninstallSystemdReloader = origReload
	})
	var removed []string
	uninstallServiceStopper = func(string) error { return nil }
	uninstallFileRemover = func(path string) error {
		removed = append(removed, filepath.ToSlash(path))
		return nil
	}
	uninstallSystemdReloader = func() error { return nil }
	cmd := NewRootCommand("test")
	cmd.SetArgs([]string{"uninstall", "--yes", "--keep-data"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, preserved := range []string{"/etc/veil", "/var/lib/veil", "/var/lib/caddy", "/var/lib/mita"} {
		for _, path := range removed {
			if path == preserved {
				t.Fatalf("--keep-data removed preserved path %s: %v", preserved, removed)
			}
		}
	}
	if !contains(removed, "/usr/local/bin/veil") || !contains(removed, "/etc/systemd/system/veil-backup.service.d") {
		t.Fatalf("--keep-data must still remove the binary and backup drop-in: %v", removed)
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

func TestUninstallPurgeRemovesState(t *testing.T) {
	origStop := uninstallServiceStopper
	origRemove := uninstallFileRemover
	origReload := uninstallSystemdReloader
	t.Cleanup(func() {
		uninstallServiceStopper = origStop
		uninstallFileRemover = origRemove
		uninstallSystemdReloader = origReload
	})
	var removed []string
	uninstallServiceStopper = func(string) error { return nil }
	uninstallFileRemover = func(path string) error {
		removed = append(removed, filepath.ToSlash(path))
		return nil
	}
	uninstallSystemdReloader = func() error { return nil }
	cmd := NewRootCommand("test")
	cmd.SetArgs([]string{"uninstall", "--yes", "--purge"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/etc/veil", "/var/lib/veil", "/var/lib/caddy", "/var/lib/mita"} {
		found := false
		for _, path := range removed {
			found = found || path == want
		}
		if !found {
			t.Fatalf("purge did not remove %s: %v", want, removed)
		}
	}
}

func TestUninstallCaddyMitaStateDirOverrides(t *testing.T) {
	origStop := uninstallServiceStopper
	origRemove := uninstallFileRemover
	origReload := uninstallSystemdReloader
	t.Cleanup(func() {
		uninstallServiceStopper = origStop
		uninstallFileRemover = origRemove
		uninstallSystemdReloader = origReload
	})
	var removed []string
	uninstallServiceStopper = func(string) error { return nil }
	uninstallFileRemover = func(path string) error {
		removed = append(removed, filepath.ToSlash(path))
		return nil
	}
	uninstallSystemdReloader = func() error { return nil }

	// Env seeds the dirs the shell fallback contract advertises.
	t.Setenv("VEIL_CADDY_STATE_DIR", "/srv/shared-caddy")
	t.Setenv("VEIL_MITA_STATE_DIR", "/srv/shared-mita")
	cmd := NewRootCommand("test")
	cmd.SetArgs([]string{"uninstall", "--yes", "--purge"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/srv/shared-caddy", "/srv/shared-mita"} {
		if !contains(removed, want) {
			t.Fatalf("env-seeded state dir %s not removed: %v", want, removed)
		}
	}
	if contains(removed, "/var/lib/caddy") || contains(removed, "/var/lib/mita") {
		t.Fatalf("default state dirs must not be removed when env overrides them: %v", removed)
	}

	// Explicit flags win over env.
	removed = nil
	cmd = NewRootCommand("test")
	cmd.SetArgs([]string{"uninstall", "--yes", "--purge", "--caddy-state-dir", "/flag/caddy", "--mita-state-dir", "/flag/mita"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/flag/caddy", "/flag/mita"} {
		if !contains(removed, want) {
			t.Fatalf("flag-provided state dir %s not removed: %v", want, removed)
		}
	}
}

// The CLI cannot invoke real systemctl in unit tests — the command-level
// coverage (systemctl stop + disable argv) lives in internal/cliflow/uninstall
// via the stubbed runner. This test locks the wiring instead: the CLI stopper
// must still be that real implementation, not a stub or a no-op.
func TestUninstallServiceStopperDelegatesToRealImplementation(t *testing.T) {
	if reflect.ValueOf(uninstallServiceStopper).Pointer() != reflect.ValueOf(uninstallflow.StopAndDisableService).Pointer() {
		t.Fatal("uninstallServiceStopper must delegate to cliflow/uninstall.StopAndDisableService")
	}
	if reflect.ValueOf(uninstallFileRemover).Pointer() != reflect.ValueOf(uninstallflow.RemovePath).Pointer() {
		t.Fatal("uninstallFileRemover must delegate to cliflow/uninstall.RemovePath")
	}
	if reflect.ValueOf(uninstallSystemdReloader).Pointer() != reflect.ValueOf(uninstallflow.ReloadSystemdDaemon).Pointer() {
		t.Fatal("uninstallSystemdReloader must delegate to cliflow/uninstall.ReloadSystemdDaemon")
	}
}

// Ensure temp dirs don't leak from tests
func init() {
	os.RemoveAll("/tmp/veil-uninstall-test")
}
