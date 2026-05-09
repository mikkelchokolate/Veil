package firewall

import (
	"strings"
	"time"

	veilruntime "github.com/veil-panel/veil/internal/runtime"
)

type RuntimeCommandRunner interface {
	Run(veilruntime.RuntimeCommandInput) veilruntime.RuntimeCommandOutput
}

type StatusReader struct {
	runner RuntimeCommandRunner
}

func NewStatusReader(runner RuntimeCommandRunner) StatusReader {
	if runner == nil {
		runner = veilruntime.NewRuntimeCommandExecutor()
	}
	return StatusReader{runner: runner}
}

func (r StatusReader) Active() (bool, error) {
	out := r.runner.Run(veilruntime.RuntimeCommandInput{Command: []string{"ufw", "status"}, Timeout: 5 * time.Second})
	if out.Err != nil || out.NotFound || out.TimedOut {
		return false, nil
	}
	return strings.Contains(out.Output, "Status: active"), nil
}
