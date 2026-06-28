package firewall

import (
	"errors"
	"strings"
	"testing"

	veilruntime "github.com/mikkelchokolate/Veil/internal/runtime"
)

type fakeCommandRunner struct {
	calls [][]string
}

func (r *fakeCommandRunner) Run(input veilruntime.RuntimeCommandInput) veilruntime.RuntimeCommandOutput {
	r.calls = append(r.calls, append([]string(nil), input.Command...))
	if len(input.Command) == 0 {
		return veilruntime.RuntimeCommandOutput{Err: errors.New("empty command"), Empty: true}
	}
	if input.Command[0] == "ufw" && len(input.Command) > 1 && input.Command[1] == "status" {
		return veilruntime.RuntimeCommandOutput{Output: "Status: inactive"}
	}
	return veilruntime.RuntimeCommandOutput{}
}

func TestUFWApplierEnablesFirewallAndAppliesRules(t *testing.T) {
	runner := &fakeCommandRunner{}
	applier := NewUFWApplierWithRunner(runner)
	if err := applier.EnsureActive(); err != nil {
		t.Fatalf("EnsureActive: %v", err)
	}
	if err := applier.ApplyRules([]Rule{
		{Command: "ufw", Args: []string{"allow", "4315/udp", "comment", "Veil Hysteria2"}},
		{Command: "ufw", Args: []string{"allow", "443/tcp", "comment", "Veil panel HTTPS"}},
	}); err != nil {
		t.Fatalf("ApplyRules: %v", err)
	}

	want := [][]string{
		{"ufw", "status"},
		{"ufw", "--force", "enable"},
		{"ufw", "allow", "4315/udp", "comment", "Veil Hysteria2"},
		{"ufw", "allow", "443/tcp", "comment", "Veil panel HTTPS"},
	}
	if len(runner.calls) != len(want) {
		t.Fatalf("calls = %v, want %v", runner.calls, want)
	}
	for i, w := range want {
		if len(runner.calls[i]) != len(w) {
			t.Fatalf("call %d = %v, want %v", i, runner.calls[i], w)
		}
		for j, v := range w {
			if runner.calls[i][j] != v {
				t.Fatalf("call %d = %v, want %v", i, runner.calls[i], w)
			}
		}
	}
}

func TestUFWApplierSkipsEnableWhenAlreadyActive(t *testing.T) {
	runner := &fakeCommandRunner{}
	activeRunner := &activeStatusRunner{fake: runner}
	applier := NewUFWApplierWithRunner(activeRunner)
	if err := applier.EnsureActive(); err != nil {
		t.Fatalf("EnsureActive: %v", err)
	}
	for _, call := range runner.calls {
		if len(call) >= 3 && call[0] == "ufw" && call[1] == "--force" && call[2] == "enable" {
			t.Fatalf("expected no ufw enable call when active, got %v", runner.calls)
		}
	}
}

func TestUFWApplierTreatsDuplicateRuleAsSuccess(t *testing.T) {
	runner := &duplicateRuleRunner{}
	applier := NewUFWApplierWithRunner(runner)
	if err := applier.ApplyRules([]Rule{{Command: "ufw", Args: []string{"allow", "4315/udp", "comment", "Veil Hysteria2"}}}); err != nil {
		t.Fatalf("ApplyRules: %v", err)
	}
}

func TestUFWApplierRejectsUnsupportedCommand(t *testing.T) {
	runner := &fakeCommandRunner{}
	applier := NewUFWApplierWithRunner(runner)
	err := applier.ApplyRules([]Rule{{Command: "iptables", Args: []string{"-A", "INPUT"}}})
	if err == nil || !strings.Contains(err.Error(), "unsupported firewall command") {
		t.Fatalf("expected unsupported command error, got %v", err)
	}
}

type activeStatusRunner struct {
	fake *fakeCommandRunner
}

func (r *activeStatusRunner) Run(input veilruntime.RuntimeCommandInput) veilruntime.RuntimeCommandOutput {
	out := r.fake.Run(input)
	if len(input.Command) >= 2 && input.Command[0] == "ufw" && input.Command[1] == "status" {
		out.Output = "Status: active"
	}
	return out
}

type duplicateRuleRunner struct{}

func (duplicateRuleRunner) Run(input veilruntime.RuntimeCommandInput) veilruntime.RuntimeCommandOutput {
	return veilruntime.RuntimeCommandOutput{
		Err:    errors.New("exit status 1"),
		Output: "Skipping adding existing rule",
	}
}
