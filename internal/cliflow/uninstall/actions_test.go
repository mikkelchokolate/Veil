package uninstall

import (
	"errors"
	"testing"

	veilruntime "github.com/mikkelchokolate/Veil/internal/runtime"
)

type fakeCommandRunner struct {
	calls [][]string
	outs  []veilruntime.RuntimeCommandOutput
}

func (r *fakeCommandRunner) Run(input veilruntime.RuntimeCommandInput) veilruntime.RuntimeCommandOutput {
	r.calls = append(r.calls, append([]string(nil), input.Command...))
	if len(r.outs) == 0 {
		return veilruntime.RuntimeCommandOutput{}
	}
	out := r.outs[0]
	r.outs = r.outs[1:]
	return out
}

func TestActionsStopAndDisableServiceRunsSystemctlCommands(t *testing.T) {
	runner := &fakeCommandRunner{}
	if err := NewActions(runner, nil).StopAndDisableService("veil.service"); err != nil {
		t.Fatalf("StopAndDisableService: %v", err)
	}
	want := [][]string{{"systemctl", "stop", "veil.service"}, {"systemctl", "disable", "veil.service"}}
	if !sameCommands(runner.calls, want) {
		t.Fatalf("calls = %+v, want %+v", runner.calls, want)
	}
}

func TestActionsStopAndDisableServiceLabelsFailingPhase(t *testing.T) {
	runner := &fakeCommandRunner{outs: []veilruntime.RuntimeCommandOutput{{}, {Err: errors.New("denied")}}}
	err := NewActions(runner, nil).StopAndDisableService("veil.service")
	if err == nil || err.Error() != "disable: denied" {
		t.Fatalf("err = %v", err)
	}
}

func TestActionsReloadsSystemdAndRemovesPaths(t *testing.T) {
	runner := &fakeCommandRunner{}
	removed := ""
	actions := NewActions(runner, func(path string) error {
		removed = path
		return nil
	})
	if err := actions.RemovePath("/tmp/veil"); err != nil {
		t.Fatalf("RemovePath: %v", err)
	}
	if removed != "/tmp/veil" {
		t.Fatalf("removed = %q", removed)
	}
	if err := actions.ReloadSystemdDaemon(); err != nil {
		t.Fatalf("ReloadSystemdDaemon: %v", err)
	}
	if !sameCommands(runner.calls, [][]string{{"systemctl", "daemon-reload"}}) {
		t.Fatalf("calls = %+v", runner.calls)
	}
}

func sameCommands(a, b [][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return false
		}
		for j := range a[i] {
			if a[i][j] != b[i][j] {
				return false
			}
		}
	}
	return true
}
