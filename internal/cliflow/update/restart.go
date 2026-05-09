package update

import (
	"time"

	veilruntime "github.com/veil-panel/veil/internal/runtime"
)

type CommandRunner interface {
	Run(veilruntime.RuntimeCommandInput) veilruntime.RuntimeCommandOutput
}

type SystemctlRestarter struct {
	runner CommandRunner
}

func NewSystemctlRestarter(runner CommandRunner) SystemctlRestarter {
	if runner == nil {
		runner = veilruntime.NewRuntimeCommandExecutor()
	}
	return SystemctlRestarter{runner: runner}
}

func RunSystemctlRestart(unit string) error {
	return NewSystemctlRestarter(nil).Restart(unit)
}

func (r SystemctlRestarter) Restart(unit string) error {
	out := r.runner.Run(veilruntime.RuntimeCommandInput{Command: []string{"systemctl", "restart", unit}, Timeout: 30 * time.Second})
	return out.Err
}
