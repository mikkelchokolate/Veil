package service

import (
	"reflect"
	"testing"
)

func TestSystemdApplyPlanForManagedUnits(t *testing.T) {
	plan := SystemdApplyPlan([]string{"veil.service", "veil-naive.service", "veil-hysteria2.service"})
	want := []SystemdAction{
		{Command: "systemctl", Args: []string{"daemon-reload"}},
		{Command: "systemctl", Args: []string{"enable", "veil.service"}},
		{Command: "systemctl", Args: []string{"enable", "veil-naive.service"}},
		{Command: "systemctl", Args: []string{"enable", "veil-hysteria2.service"}},
		{Command: "systemctl", Args: []string{"restart", "veil.service"}},
		{Command: "systemctl", Args: []string{"restart", "veil-naive.service"}},
		{Command: "systemctl", Args: []string{"restart", "veil-hysteria2.service"}},
	}
	if !reflect.DeepEqual(plan, want) {
		t.Fatalf("unexpected plan:\n got: %#v\nwant: %#v", plan, want)
	}
}

func TestSystemdApplyPlanIgnoresEmptyUnits(t *testing.T) {
	plan := SystemdApplyPlan([]string{"", "veil.service"})
	if len(plan) != 3 {
		t.Fatalf("expected daemon-reload + enable + restart, got %#v", plan)
	}
}
