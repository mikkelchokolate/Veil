package uninstall

import (
	"errors"
	"testing"

	veilruntime "github.com/mikkelchokolate/Veil/internal/runtime"
)

func TestActionsStopAndDisableServiceStopError(t *testing.T) {
	runner := &fakeCommandRunner{outs: []veilruntime.RuntimeCommandOutput{{Err: errors.New("stop boom")}}}
	err := NewActions(runner, nil).StopAndDisableService("veil.service")
	if err == nil || err.Error() != "stop: stop boom" {
		t.Fatalf("err = %v", err)
	}
}

func TestActionsReloadSystemdDaemonError(t *testing.T) {
	runner := &fakeCommandRunner{outs: []veilruntime.RuntimeCommandOutput{{Err: errors.New("reload boom")}}}
	err := NewActions(runner, nil).ReloadSystemdDaemon()
	if err == nil || err.Error() != "daemon-reload: reload boom" {
		t.Fatalf("err = %v", err)
	}
}

func TestNewActionsUsesDefaultRunner(t *testing.T) {
	runner := &fakeCommandRunner{}
	actions := NewActions(runner, nil)
	if err := actions.StopAndDisableService("veil.service"); err != nil {
		t.Fatalf("StopAndDisableService: %v", err)
	}
	want := [][]string{{"systemctl", "stop", "veil.service"}, {"systemctl", "disable", "veil.service"}}
	if !sameCommands(runner.calls, want) {
		t.Fatalf("calls = %+v, want %+v", runner.calls, want)
	}
}

func TestNewActionsUsesDefaultFileRemover(t *testing.T) {
	var removed string
	actions := NewActions(nil, func(path string) error {
		removed = path
		return nil
	})
	if err := actions.RemovePath("/tmp/veil"); err != nil {
		t.Fatalf("RemovePath: %v", err)
	}
	if removed != "/tmp/veil" {
		t.Fatalf("removed = %q", removed)
	}
}

func TestDefaultDependenciesReturnsNonNil(t *testing.T) {
	deps := DefaultDependencies()
	if deps.ServiceStopper == nil || deps.FileRemover == nil || deps.SystemdReloader == nil {
		t.Fatalf("deps has nil function: %+v", deps)
	}
}

func TestPackageLevelStopAndDisableService(t *testing.T) {
	old := defaultActions
	defer func() { defaultActions = old }()
	runner := &fakeCommandRunner{}
	defaultActions = func() Actions { return NewActions(runner, nil) }
	if err := StopAndDisableService("veil.service"); err != nil {
		t.Fatalf("StopAndDisableService: %v", err)
	}
	want := [][]string{{"systemctl", "stop", "veil.service"}, {"systemctl", "disable", "veil.service"}}
	if !sameCommands(runner.calls, want) {
		t.Fatalf("calls = %+v, want %+v", runner.calls, want)
	}
}

func TestPackageLevelRemovePath(t *testing.T) {
	old := defaultActions
	defer func() { defaultActions = old }()
	var removed string
	defaultActions = func() Actions {
		return NewActions(nil, func(path string) error {
			removed = path
			return nil
		})
	}
	if err := RemovePath("/tmp/veil"); err != nil {
		t.Fatalf("RemovePath: %v", err)
	}
	if removed != "/tmp/veil" {
		t.Fatalf("removed = %q", removed)
	}
}

func TestPackageLevelReloadSystemdDaemon(t *testing.T) {
	old := defaultActions
	defer func() { defaultActions = old }()
	runner := &fakeCommandRunner{}
	defaultActions = func() Actions { return NewActions(runner, nil) }
	if err := ReloadSystemdDaemon(); err != nil {
		t.Fatalf("ReloadSystemdDaemon: %v", err)
	}
	want := [][]string{{"systemctl", "daemon-reload"}}
	if !sameCommands(runner.calls, want) {
		t.Fatalf("calls = %+v, want %+v", runner.calls, want)
	}
}
