package service

import "time"

type SystemdServiceStatusCommand struct {
	unit string
}

func NewSystemdServiceStatusCommand(unit string) SystemdServiceStatusCommand {
	return SystemdServiceStatusCommand{unit: unit}
}

func (SystemdServiceStatusCommand) Name() string { return "systemctl" }

func (c SystemdServiceStatusCommand) Args() []string {
	return []string{
		"show",
		c.unit,
		"--property=LoadState",
		"--property=ActiveState",
		"--property=SubState",
		"--no-page",
	}
}

func (SystemdServiceStatusCommand) Timeout() time.Duration { return 5 * time.Second }
