package service

import "os/exec"

type SystemdAction struct {
	Command string
	Args    []string
}

type CommandRunner interface {
	Run(command string, args ...string) error
}

type ExecRunner struct{}

func (ExecRunner) Run(command string, args ...string) error {
	cmd := exec.Command(command, args...)
	return cmd.Run()
}

func SystemdApplyPlan(units []string) []SystemdAction {
	clean := make([]string, 0, len(units))
	for _, unit := range units {
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

func RunSystemdActions(runner CommandRunner, actions []SystemdAction) error {
	for _, action := range actions {
		if err := runner.Run(action.Command, action.Args...); err != nil {
			return err
		}
	}
	return nil
}
