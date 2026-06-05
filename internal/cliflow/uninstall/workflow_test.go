package uninstall

import (
	"bytes"
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
	if !strings.Contains(out.String(), "Veil uninstall plan") || !strings.Contains(out.String(), "veil.service") {
		t.Fatalf("output = %s", out.String())
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
	if !contains(stopped, "veil-mieru.service") || !contains(removed, "/tmp/systemd/veil-mieru.service") || !contains(removed, "/tmp/bin/veil") || contains(removed, "/tmp/etc") || contains(removed, "/tmp/var") || !reloaded {
		t.Fatalf("stopped=%+v removed=%+v reloaded=%v", stopped, removed, reloaded)
	}
	if !strings.Contains(out.String(), "Preserved state:") {
		t.Fatalf("output does not report preserved state: %s", out.String())
	}
	if !strings.Contains(out.String(), "Uninstalled Veil") {
		t.Fatalf("output = %s", out.String())
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

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
