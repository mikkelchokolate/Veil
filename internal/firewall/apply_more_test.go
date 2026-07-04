package firewall

import (
	"errors"
	"reflect"
	"testing"

	veilruntime "github.com/mikkelchokolate/Veil/internal/runtime"
)

func TestNewUFWApplierCreatesDefaultRunner(t *testing.T) {
	applier := NewUFWApplier()
	if applier.runner == nil {
		t.Fatal("expected non-nil runner")
	}
}

func TestNewUFWApplierWithRunnerFallsBackToDefaultOnNil(t *testing.T) {
	applier := NewUFWApplierWithRunner(nil)
	if applier.runner == nil {
		t.Fatal("expected non-nil runner when nil is passed")
	}
}

func TestEnsureActiveReturnsErrorWhenEnableFails(t *testing.T) {
	runner := &enableFailRunner{fake: &fakeCommandRunner{}}
	applier := NewUFWApplierWithRunner(runner)
	err := applier.EnsureActive()
	if err == nil || !errors.Is(err, errTestUFWEnable) {
		t.Fatalf("expected ufw enable error, got %v", err)
	}
}

type enableFailRunner struct {
	fake *fakeCommandRunner
}

func (r *enableFailRunner) Run(input veilruntime.RuntimeCommandInput) veilruntime.RuntimeCommandOutput {
	if len(input.Command) >= 3 && input.Command[0] == "ufw" && input.Command[1] == "--force" && input.Command[2] == "enable" {
		return veilruntime.RuntimeCommandOutput{Err: errTestUFWEnable, Output: "enable failed"}
	}
	return r.fake.Run(input)
}

var errTestUFWEnable = errors.New("test ufw enable failure")

func TestApplyRulesReturnsErrorOnNonDuplicateFailure(t *testing.T) {
	runner := &applyFailRunner{}
	applier := NewUFWApplierWithRunner(runner)
	err := applier.ApplyRules([]Rule{{Command: "ufw", Args: []string{"allow", "1234/tcp", "comment", "test"}}})
	if err == nil || !errors.Is(err, errTestApplyRule) {
		t.Fatalf("expected apply rule error, got %v", err)
	}
}

type applyFailRunner struct{}

func (applyFailRunner) Run(input veilruntime.RuntimeCommandInput) veilruntime.RuntimeCommandOutput {
	return veilruntime.RuntimeCommandOutput{Err: errTestApplyRule, Output: "some unrelated failure"}
}

var errTestApplyRule = errors.New("test apply rule failure")

func TestUFWRulesFromResponses(t *testing.T) {
	responses := []RuleResponse{
		{Port: 443, Protocol: "tcp", Service: "Veil panel HTTPS"},
		{Port: 8443, Protocol: "udp", Service: "Veil Hysteria2"},
		{Port: -1, Protocol: "tcp", Service: "invalid port"},
		{Port: 80, Protocol: "icmp", Service: "invalid protocol"},
		{Port: 2096, Protocol: "tcp", Service: "Veil panel"},
	}
	want := []Rule{
		{Command: "ufw", Args: []string{"allow", "443/tcp", "comment", "Veil panel HTTPS"}},
		{Command: "ufw", Args: []string{"allow", "8443/udp", "comment", "Veil Hysteria2"}},
		{Command: "ufw", Args: []string{"allow", "2096/tcp", "comment", "Veil panel"}},
	}
	got := UFWRulesFromResponses(responses)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UFWRulesFromResponses() = %#v, want %#v", got, want)
	}
}

func TestUFWRulesFromResponsesEmpty(t *testing.T) {
	got := UFWRulesFromResponses(nil)
	if len(got) != 0 {
		t.Fatalf("expected empty rules, got %#v", got)
	}
}
