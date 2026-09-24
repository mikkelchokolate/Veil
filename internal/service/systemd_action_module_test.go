package service

import (
	"reflect"
	"strings"
	"testing"
)

func TestSystemdActionModulePlansAndRunsActions(t *testing.T) {
	module := NewSystemdActionModule([]string{"veil.service"})
	actions := module.Plan()
	wantActions := []SystemdAction{
		{Command: "systemctl", Args: []string{"daemon-reload"}},
		{Command: "systemctl", Args: []string{"enable", "veil.service"}},
		{Command: "systemctl", Args: []string{"restart", "veil.service"}},
	}
	if !reflect.DeepEqual(actions, wantActions) {
		t.Fatalf("actions = %+v, want %+v", actions, wantActions)
	}
	var ran []string
	runner := CommandRunnerFunc(func(command string, args ...string) error {
		ran = append(ran, command+":"+strings.Join(args, " "))
		return nil
	})
	if err := module.Run(runner, actions); err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantRan := []string{"systemctl:daemon-reload", "systemctl:enable veil.service", "systemctl:restart veil.service"}
	if !reflect.DeepEqual(ran, wantRan) {
		t.Fatalf("ran = %+v, want %+v", ran, wantRan)
	}
}
