package service

import (
	"context"
	"os/exec"
	"time"
)

type SystemdActionModule struct {
	units []string
}

type CommandRunnerFunc func(command string, args ...string) error

func (f CommandRunnerFunc) Run(command string, args ...string) error { return f(command, args...) }

func NewSystemdActionModule(units []string) SystemdActionModule {
	return SystemdActionModule{units: units}
}

func (m SystemdActionModule) Plan() []SystemdAction {
	clean := make([]string, 0, len(m.units))
	for _, unit := range m.units {
		if unit != "" {
			clean = append(clean, unit)
		}
	}
	if len(clean) == 0 {
		return nil
	}
	actions := []SystemdAction{{Command: "systemctl", Args: []string{"daemon-reload"}}}
	for _, unit := range clean {
		actions = append(actions, SystemdAction{Command: "systemctl", Args: []string{"enable", unit}})
	}
	for _, unit := range clean {
		actions = append(actions, SystemdAction{Command: "systemctl", Args: []string{"restart", unit}})
	}
	return actions
}

func (SystemdActionModule) Run(runner CommandRunner, actions []SystemdAction) error {
	for _, action := range actions {
		if err := runner.Run(action.Command, action.Args...); err != nil {
			return err
		}
	}
	return nil
}

type SystemdExecRunner struct{}

func (SystemdExecRunner) Run(command string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), systemdCommandTimeout(command, args...))
	defer cancel()
	cmd := exec.CommandContext(ctx, command, args...)
	return cmd.Run()
}

func systemdCommandTimeout(command string, args ...string) time.Duration {
	timeout := 10 * time.Second
	if command == "systemctl" && len(args) > 0 {
		switch args[0] {
		case "daemon-reload", "enable", "disable":
			timeout = 10 * time.Second
		case "restart":
			timeout = 30 * time.Second
		}
	}
	return timeout
}
