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
	Error       string
}

type ServiceRuntimeStatus = RuntimeStatus

func ReadSystemdServiceStatus(unit string) RuntimeStatus {
	command := NewSystemdServiceStatusCommand(unit)
	ctx, cancel := context.WithTimeout(context.Background(), command.Timeout())
	defer cancel()
	output, err := exec.CommandContext(ctx, command.Name(), command.Args()...).CombinedOutput()
	status := NewSystemdServiceStatusParser().Parse(unit, string(output))
	if err != nil {
		status.Error = strings.TrimSpace(string(output))
		if status.Error == "" {
			status.Error = err.Error()
		}
	}
	return status
}
