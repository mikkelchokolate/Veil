package update

import (
	"errors"
	"testing"
	"time"

	veilruntime "github.com/mikkelchokolate/Veil/internal/runtime"
)

func TestNewSystemctlRestarterUsesDefaultRunnerWhenNil(t *testing.T) {
	r := NewSystemctlRestarter(nil)
	if r.runner == nil {
		t.Fatal("expected default runner")
	}
}

func TestRunSystemctlRestartReturnsErrorForMissingUnit(t *testing.T) {
	err := RunSystemctlRestart("veil-update-does-not-exist.service")
	if err == nil {
		t.Fatal("expected error restarting nonexistent unit")
	}
}

func TestSystemctlRestarterPropagatesRunnerError(t *testing.T) {
	want := errors.New("boom")
	runner := &fakeRestartRunner{out: veilruntime.RuntimeCommandOutput{Err: want}}
	err := NewSystemctlRestarter(runner).Restart("veil.service")
	if !errors.Is(err, want) {
		t.Fatalf("err = %v", err)
	}
}

func TestSystemctlRestarterPassesTimeout(t *testing.T) {
	runner := &fakeRestartRunner{}
	_ = NewSystemctlRestarter(runner).Restart("veil.service")
	if runner.input.Timeout != 30*time.Second {
		t.Fatalf("timeout = %v", runner.input.Timeout)
	}
}
