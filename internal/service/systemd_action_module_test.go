package service

import "testing"

func TestSystemdActionModulePlansAndRunsActions(t *testing.T) {
	module := NewSystemdActionModule([]string{"veil.service"})
	actions := module.Plan()
	if len(actions) != 3 {
		t.Fatalf("actions = %+v", actions)
	}
	var ran []string
	runner := CommandRunnerFunc(func(command string, args ...string) error {
		ran = append(ran, command+":"+args[0])
		return nil
	})
	if err := module.Run(runner, actions); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(ran) != 3 || ran[0] != "systemctl:daemon-reload" {
		t.Fatalf("ran = %+v", ran)
	}
}
