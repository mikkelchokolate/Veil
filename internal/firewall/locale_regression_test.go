package firewall

import (
	"testing"

	veilruntime "github.com/mikkelchokolate/Veil/internal/runtime"
)

type localeCaptureRunner struct {
	input veilruntime.RuntimeCommandInput
}

func (r *localeCaptureRunner) Run(input veilruntime.RuntimeCommandInput) veilruntime.RuntimeCommandOutput {
	r.input = input
	return veilruntime.RuntimeCommandOutput{Output: "Status: active"}
}

func TestUFWStatusForcesStableLocale(t *testing.T) {
	runner := &localeCaptureRunner{}
	active, err := NewStatusReader(runner).Active()
	if err != nil || !active {
		t.Fatalf("active=%v err=%v", active, err)
	}
	seen := map[string]bool{}
	for _, item := range runner.input.Env {
		seen[item] = true
	}
	if !seen["LC_ALL=C"] || !seen["LANG=C"] {
		t.Fatalf("environment=%v, want LC_ALL=C and LANG=C", runner.input.Env)
	}
}
