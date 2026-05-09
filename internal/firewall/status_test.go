package firewall

import (
	"errors"
	"testing"
	"time"

	veilruntime "github.com/veil-panel/veil/internal/runtime"
)

type fakeStatusRunner struct {
	input veilruntime.RuntimeCommandInput
	out   veilruntime.RuntimeCommandOutput
}

func (r *fakeStatusRunner) Run(input veilruntime.RuntimeCommandInput) veilruntime.RuntimeCommandOutput {
	r.input = input
	return r.out
}

func TestStatusReaderBuildsBoundedUFWStatusCommand(t *testing.T) {
	runner := &fakeStatusRunner{out: veilruntime.RuntimeCommandOutput{Output: "Status: active"}}
	active, err := NewStatusReader(runner).Active()
	if err != nil {
		t.Fatalf("Active: %v", err)
	}
	if !active {
		t.Fatal("expected active firewall")
	}
	if !sameCommand(runner.input.Command, []string{"ufw", "status"}) || runner.input.Timeout != 5*time.Second {
		t.Fatalf("input = %+v", runner.input)
	}
}

func TestStatusReaderTreatsUnavailableUFWAsInactive(t *testing.T) {
	active, err := NewStatusReader(&fakeStatusRunner{out: veilruntime.RuntimeCommandOutput{Err: errors.New("ufw missing")}}).Active()
	if err != nil {
		t.Fatalf("Active err = %v", err)
	}
	if active {
		t.Fatal("expected inactive firewall")
	}
}

func sameCommand(a, b []string) bool {
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
