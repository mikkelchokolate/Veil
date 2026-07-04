package uninstall

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRunRequiresYes(t *testing.T) {
	var out, errOut bytes.Buffer
	called := false
	err := Run(Options{}, &out, &errOut, Dependencies{
		ServiceStopper:  func(string) error { called = true; return nil },
		FileRemover:     func(string) error { called = true; return nil },
		SystemdReloader: func() error { called = true; return nil },
	})
	if err == nil {
		t.Fatal("expected error when --yes is missing")
	}
	if called {
		t.Fatal("missing --yes should not call dependencies")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunServiceStopperError(t *testing.T) {
	var out, errOut bytes.Buffer
	err := Run(Options{
		Yes: true, EtcDir: t.TempDir(), VarDir: t.TempDir(),
		SystemdDir: t.TempDir(), InstallDir: t.TempDir(),
	}, &out, &errOut, Dependencies{
		ServiceStopper: func(service string) error {
			if service == "veil.service" {
				return errors.New("stop failed")
			}
			return nil
		},
		FileRemover:     func(string) error { return nil },
		SystemdReloader: func() error { return nil },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(errOut.String(), "warning: service veil.service: stop failed") {
		t.Fatalf("errOut = %s", errOut.String())
	}
}

func TestRunFileRemoverError(t *testing.T) {
	var out, errOut bytes.Buffer
	installDir := t.TempDir()
	err := Run(Options{
		Yes: true, EtcDir: t.TempDir(), VarDir: t.TempDir(),
		SystemdDir: t.TempDir(), InstallDir: installDir,
	}, &out, &errOut, Dependencies{
		ServiceStopper: func(string) error { return nil },
		FileRemover: func(path string) error {
			if path == BinaryPath(Options{InstallDir: installDir}) {
				return nil
			}
			return errors.New("remove failed")
		},
		SystemdReloader: func() error { return nil },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(errOut.String(), "warning: remove") {
		t.Fatalf("errOut = %s", errOut.String())
	}
}

func TestRunSystemdReloaderError(t *testing.T) {
	var out, errOut bytes.Buffer
	err := Run(Options{
		Yes: true, EtcDir: t.TempDir(), VarDir: t.TempDir(),
		SystemdDir: t.TempDir(), InstallDir: t.TempDir(),
	}, &out, &errOut, Dependencies{
		ServiceStopper:  func(string) error { return nil },
		FileRemover:     func(string) error { return nil },
		SystemdReloader: func() error { return errors.New("reload failed") },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(errOut.String(), "warning: systemd daemon-reload: reload failed") {
		t.Fatalf("errOut = %s", errOut.String())
	}
}

func TestPlanPreservesData(t *testing.T) {
	plan := Plan(Options{
		KeepData: true, EtcDir: "/tmp/etc", VarDir: "/tmp/var",
		SystemdDir: "/tmp/systemd", InstallDir: "/tmp/bin",
	})
	if !strings.Contains(plan, "Preserved state:") {
		t.Fatalf("plan = %s", plan)
	}
	if strings.Contains(plan, "Remove configuration and state:") {
		t.Fatalf("plan should not show removal when preserving data: %s", plan)
	}
}

func TestPlanPurgeOverridesKeepData(t *testing.T) {
	plan := Plan(Options{
		KeepData: true, Purge: true, EtcDir: "/tmp/etc", VarDir: "/tmp/var",
		SystemdDir: "/tmp/systemd", InstallDir: "/tmp/bin",
	})
	if !strings.Contains(plan, "Remove configuration and state:") {
		t.Fatalf("plan = %s", plan)
	}
	if strings.Contains(plan, "Preserved state:") {
		t.Fatalf("plan should show removal when purging: %s", plan)
	}
}
