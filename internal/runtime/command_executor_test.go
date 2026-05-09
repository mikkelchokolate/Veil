package runtime

import "testing"

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
