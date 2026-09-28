package service

import (
	"context"
	"os/exec"
	"strings"
)

type RuntimeStatus struct {
	Unit        string
	LoadState   string
	ActiveState string
	SubState    string
	// UnitFileState is the systemd enablement state ("enabled", "disabled",
	// "static", ...) — apply needs it to drive desired-state teardown and to
	// restore a unit's previous lifecycle state during rollback (#1133/#1135).
	UnitFileState          string
	MainPID                int
	ExecMainStartMonotonic uint64
	ExecutableDigest       string
	Error                  string
}

type ServiceRuntimeStatus = RuntimeStatus

// execCommandContext is swapped during tests so ReadSystemdServiceStatus can be
// exercised without invoking the real systemctl binary.
var execCommandContext = exec.CommandContext

func ReadSystemdServiceStatus(unit string) RuntimeStatus {
	command := NewSystemdServiceStatusCommand(unit)
	ctx, cancel := context.WithTimeout(context.Background(), command.Timeout())
	defer cancel()
	output, err := execCommandContext(ctx, command.Name(), command.Args()...).CombinedOutput()
	status := NewSystemdServiceStatusParser().Parse(unit, string(output))
	if err != nil {
		status.Error = strings.TrimSpace(string(output))
		if status.Error == "" {
			status.Error = err.Error()
		}
	}
	return status
}
