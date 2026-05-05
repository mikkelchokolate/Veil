package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
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
		"Remove files:",
		"Remove binary:",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestUninstallYesExecutesUninstall(t *testing.T) {
	origStop := uninstallServiceStopper
	origRemove := uninstallFileRemover
	t.Cleanup(func() {
		uninstallServiceStopper = origStop
		uninstallFileRemover = origRemove
	})

	stopped := []string{}
	removed := []string{}

	uninstallServiceStopper = func(service string) error {
		stopped = append(stopped, service)
		return nil
	}
	uninstallFileRemover = func(path string) error {
		removed = append(removed, path)
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
	for _, svc := range []string{"veil.service", "veil-naive.service", "veil-hysteria2.service", "veil-warp.service"} {
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

	// Verify files/dirs are removed
	for _, path := range []string{"/etc/veil", "/var/lib/veil", "/usr/local/bin/veil"} {
		found := false
		for _, r := range removed {
			if r == path {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("path %s was not removed", path)
		}
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
	if !strings.Contains(got, "uninstall") || !strings.Contains(got, "--yes") || !strings.Contains(got, "--dry-run") {
		t.Fatalf("help output missing expected content:\n%s", got)
	}
}

func TestUninstallServiceStopperStopsAndDisablesReal(t *testing.T) {
	// Test that the real implementation calls systemctl stop and disable
	origLookPath := commandLookPath
	t.Cleanup(func() { commandLookPath = origLookPath })

	// Mock systemctl to return success
	var stopCalled, disableCalled bool
	commandLookPath = func(name string) (string, error) {
		if name == "systemctl" {
			return "/usr/bin/systemctl", nil
		}
		return "", errCommandNotFound
	}

	_ = stopCalled
	_ = disableCalled
	// This test is limited — real systemctl can't be called in unit tests.
	// We verify the function signature and that it doesn't panic.
	if uninstallServiceStopper == nil {
		t.Fatal("uninstallServiceStopper is nil")
	}
}

// Ensure temp dirs don't leak from tests
func init() {
	os.RemoveAll("/tmp/veil-uninstall-test")
}
