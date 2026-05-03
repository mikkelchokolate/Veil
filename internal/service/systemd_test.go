package service

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
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

type stubRunner struct {
	errOn map[string]error
	calls []string
}

func (r *stubRunner) Run(command string, args ...string) error {
	r.calls = append(r.calls, command)
	for i, a := range args {
		r.calls = append(r.calls, a)
		if err, ok := r.errOn[a]; ok && i == 0 {
			return err
		}
	}
	return nil
}

func TestRunSystemdActionsWithoutActionsReturnsNil(t *testing.T) {
	runner := &stubRunner{}
	err := RunSystemdActions(runner, nil)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("expected no calls, got %#v", runner.calls)
	}
}

func TestRunSystemdActionsExecutesAllActions(t *testing.T) {
	runner := &stubRunner{}
	actions := []SystemdAction{
		{Command: "systemctl", Args: []string{"daemon-reload"}},
		{Command: "systemctl", Args: []string{"enable", "veil.service"}},
	}
	err := RunSystemdActions(runner, actions)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	want := []string{"systemctl", "daemon-reload", "systemctl", "enable", "veil.service"}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("unexpected calls:\n got: %#v\nwant: %#v", runner.calls, want)
	}
}

func TestRunSystemdActionsReturnsErrorOnFirstFailure(t *testing.T) {
	runner := &stubRunner{
		errOn: map[string]error{"daemon-reload": errors.New("failed")},
	}
	actions := []SystemdAction{
		{Command: "systemctl", Args: []string{"daemon-reload"}},
		{Command: "systemctl", Args: []string{"enable", "veil.service"}},
	}
	err := RunSystemdActions(runner, actions)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := []string{"systemctl", "daemon-reload"}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("expected only first call before error:\n got: %#v\nwant: %#v", runner.calls, want)
	}
}

func TestRunSystemdActionsReturnsErrorOnLaterFailure(t *testing.T) {
	runner := &stubRunner{
		errOn: map[string]error{"enable": errors.New("unit not found")},
	}
	actions := []SystemdAction{
		{Command: "systemctl", Args: []string{"daemon-reload"}},
		{Command: "systemctl", Args: []string{"enable", "veil.service"}},
		{Command: "systemctl", Args: []string{"restart", "veil.service"}},
	}
	err := RunSystemdActions(runner, actions)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := []string{"systemctl", "daemon-reload", "systemctl", "enable"}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("expected calls up to failure:\n got: %#v\nwant: %#v", runner.calls, want)
	}
}

func TestSystemdCommandTimeout(t *testing.T) {
	runner := ExecRunner{}

	done := make(chan error, 1)
	go func() {
		done <- runner.Run("sleep", "35")
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected timeout error for slow command, got nil")
		}
		// Should be a context deadline exceeded error
		if !strings.Contains(err.Error(), "deadline") && !strings.Contains(err.Error(), "killed") {
			t.Fatalf("expected deadline/killed error, got: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("command was not killed within expected timeout window (no context deadline)")
	}
}

func TestSystemdCommandTimeoutForSystemctl(t *testing.T) {
	runner := ExecRunner{}

	done := make(chan error, 1)
	go func() {
		// simulate a slow daemon-reload by running sleep with systemctl-like timing
		done <- runner.Run("sleep", "15")
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected timeout error for slow systemctl command, got nil")
		}
		if !strings.Contains(err.Error(), "deadline") && !strings.Contains(err.Error(), "killed") {
			t.Fatalf("expected deadline/killed error, got: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("systemctl command was not killed within expected timeout window")
	}
}
