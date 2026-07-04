package runtime

import (
	"strings"
	"testing"
	"time"
)

func TestRuntimeCommandExecutorReportsEmptyAndMissingCommand(t *testing.T) {
	executor := NewRuntimeCommandExecutor()
	empty := executor.Run(RuntimeCommandInput{})
	if !empty.Empty || empty.Err == nil || empty.Err.Error() != "command is empty" {
		t.Fatalf("empty command result = %+v err=%v", empty, empty.Err)
	}

	missing := executor.Run(RuntimeCommandInput{Command: []string{"definitely-missing-veil-command"}})
	if !missing.NotFound || missing.Err == nil || missing.Err.Error() != "definitely-missing-veil-command not found" {
		t.Fatalf("missing command result = %+v err=%v", missing, missing.Err)
	}
}

func TestRuntimeCommandExecutorRunsCommandAndCapturesOutput(t *testing.T) {
	executor := NewRuntimeCommandExecutor()
	result := executor.Run(RuntimeCommandInput{Command: []string{"echo", "hello veil"}})
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if result.Output != "hello veil" {
		t.Fatalf("output = %q", result.Output)
	}
	if result.Empty || result.NotFound || result.TimedOut {
		t.Fatalf("unexpected flags = %+v", result)
	}
}

func TestRuntimeCommandExecutorAppliesCustomEnv(t *testing.T) {
	executor := NewRuntimeCommandExecutor()
	result := executor.Run(RuntimeCommandInput{
		Command: []string{"sh", "-c", "echo -n $VEIL_TEST_VAR"},
		Env:     []string{"VEIL_TEST_VAR=42"},
	})
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if result.Output != "42" {
		t.Fatalf("output = %q", result.Output)
	}
}

func TestRuntimeCommandExecutorAppliesTimeout(t *testing.T) {
	executor := NewRuntimeCommandExecutor()
	result := executor.Run(RuntimeCommandInput{
		Command: []string{"sleep", "5"},
		Timeout: 50 * time.Millisecond,
	})
	if !result.TimedOut {
		t.Fatalf("expected timeout, got %+v", result)
	}
	if result.Err == nil || !strings.Contains(result.Err.Error(), "context deadline exceeded") {
		t.Fatalf("expected deadline error, got %v", result.Err)
	}
}

func TestRuntimeCommandExecutorDefaultsTimeout(t *testing.T) {
	executor := NewRuntimeCommandExecutor()
	start := time.Now()
	result := executor.Run(RuntimeCommandInput{Command: []string{"true"}, Timeout: -1 * time.Second})
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("default timeout was not applied")
	}
}
