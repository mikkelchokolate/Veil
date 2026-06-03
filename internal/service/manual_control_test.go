package service

import (
	"errors"
	"testing"

	veilruntime "github.com/mikkelchokolate/Veil/internal/runtime"
)

type fakeManualControlRunner struct {
	outputs []veilruntime.RuntimeCommandOutput
	calls   [][]string
}

func (r *fakeManualControlRunner) Run(input veilruntime.RuntimeCommandInput) veilruntime.RuntimeCommandOutput {
	r.calls = append(r.calls, append([]string(nil), input.Command...))
	if len(r.outputs) == 0 {
		return veilruntime.RuntimeCommandOutput{Command: input.Command}
	}
	out := r.outputs[0]
	r.outputs = r.outputs[1:]
	return out
}

func TestManualServiceControlRunsMappedManagedRuntimeRestart(t *testing.T) {
	runner := &fakeManualControlRunner{outputs: []veilruntime.RuntimeCommandOutput{{Output: "restarted"}}}
	control := NewManualServiceControl(NewManagedRuntimeCatalog([]ManagedRuntime{{ActionName: "mieru", Unit: "veil-mieru.service", ManualRestart: true}}), runner)
	result := control.Run("mieru", "restart")
	if !result.Success || result.Service != "mieru" || result.Action != "restart" || result.Output != "restarted" {
		t.Fatalf("result = %+v", result)
	}
	if len(runner.calls) != 1 || !equalStrings(runner.calls[0], []string{"systemctl", "restart", "veil-mieru.service"}) {
		t.Fatalf("calls = %+v", runner.calls)
	}
}

func TestManualServiceControlRejectsUnknownServiceBeforeRunningCommand(t *testing.T) {
	runner := &fakeManualControlRunner{}
	control := NewManualServiceControl(NewManagedRuntimeCatalog([]ManagedRuntime{{ActionName: "mieru", Unit: "veil-mieru.service", ManualRestart: true}}), runner)
	result := control.Run("unknown", "restart")
	if result.Success || result.Error != "unknown service: unknown" {
		t.Fatalf("result = %+v", result)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("unexpected calls = %+v", runner.calls)
	}
}

func TestManualServiceControlReportsCommandFailure(t *testing.T) {
	runner := &fakeManualControlRunner{outputs: []veilruntime.RuntimeCommandOutput{{Output: "boom", Err: errors.New("exit status 1")}}}
	control := NewManualServiceControl(NewManagedRuntimeCatalog([]ManagedRuntime{{ActionName: "caddy", Unit: "veil-naive.service", ManualRestart: true}}), runner)
	result := control.Run("caddy", "restart")
	if result.Success || result.Output != "boom" || result.Error != "exit status 1" {
		t.Fatalf("result = %+v", result)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
