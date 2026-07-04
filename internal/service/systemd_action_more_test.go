package service

import (
	"testing"
	"time"
)

func TestSystemdActionModulePlanReturnsNilForEmptyUnits(t *testing.T) {
	module := NewSystemdActionModule(nil)
	if actions := module.Plan(); actions != nil {
		t.Fatalf("expected nil plan, got %+v", actions)
	}
	module = NewSystemdActionModule([]string{""})
	if actions := module.Plan(); actions != nil {
		t.Fatalf("expected nil plan for only empty unit, got %+v", actions)
	}
}

func TestSystemdCommandTimeoutBranches(t *testing.T) {
	tests := []struct {
		command string
		args    []string
		want    time.Duration
	}{
		{"systemctl", []string{"daemon-reload"}, 10 * time.Second},
		{"systemctl", []string{"enable", "veil.service"}, 10 * time.Second},
		{"systemctl", []string{"disable", "veil.service"}, 10 * time.Second},
		{"systemctl", []string{"restart", "veil.service"}, 30 * time.Second},
		{"systemctl", []string{"status", "veil.service"}, 10 * time.Second},
		{"journalctl", []string{"-u", "veil.service"}, 10 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.command+"_"+tt.args[0], func(t *testing.T) {
			if got := systemdCommandTimeout(tt.command, tt.args...); got != tt.want {
				t.Fatalf("systemdCommandTimeout(%q, %v) = %v, want %v", tt.command, tt.args, got, tt.want)
			}
		})
	}
}

func TestSystemdExecRunnerRunsFastCommand(t *testing.T) {
	runner := SystemdExecRunner{}
	if err := runner.Run("true"); err != nil {
		t.Fatalf("expected true to succeed: %v", err)
	}
}
