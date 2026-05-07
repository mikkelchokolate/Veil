package service

type SystemdAction struct {
	Command string
	Args    []string
}

type CommandRunner interface {
	Run(command string, args ...string) error
}

type ExecRunner struct{}

func (ExecRunner) Run(command string, args ...string) error {
	return SystemdExecRunner{}.Run(command, args...)
}

func SystemdApplyPlan(units []string) []SystemdAction {
	return NewSystemdActionModule(units).Plan()
}

func RunSystemdActions(runner CommandRunner, actions []SystemdAction) error {
	return NewSystemdActionModule(nil).Run(runner, actions)
}
