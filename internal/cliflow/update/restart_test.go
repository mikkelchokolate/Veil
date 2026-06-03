package update

import (
	"errors"
	"testing"
	"time"

	veilruntime "github.com/mikkelchokolate/Veil/internal/runtime"
)

type fakeRestartRunner struct {
	input veilruntime.RuntimeCommandInput
	out   veilruntime.RuntimeCommandOutput
}

func (r *fakeRestartRunner) Run(input veilruntime.RuntimeCommandInput) veilruntime.RuntimeCommandOutput {
	r.input = input
	return r.out
}

func TestSystemctlRestarterRestartsUnitWithTimeout(t *testing.T) {
	runner := &fakeRestartRunner{}
	if err := NewSystemctlRestarter(runner).Restart("veil.service"); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if !sameCommand(runner.input.Command, []string{"systemctl", "restart", "veil.service"}) || runner.input.Timeout != 30*time.Second {
		t.Fatalf("input = %+v", runner.input)
	}
}

func TestSystemctlRestarterReturnsCommandError(t *testing.T) {
	err := NewSystemctlRestarter(&fakeRestartRunner{out: veilruntime.RuntimeCommandOutput{Err: errors.New("denied")}}).Restart("veil.service")
	if err == nil || err.Error() != "denied" {
		t.Fatalf("err = %v", err)
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
