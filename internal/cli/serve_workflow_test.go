package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunServeWorkflowRejectsNonLoopbackWithoutAuth(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := runServeWorkflow(cmd, serveWorkflowOptions{Version: "test", Listen: "0.0.0.0:2096"})
	if err == nil {
		t.Fatalf("expected auth binding error")
	}
	if !strings.Contains(err.Error(), "API auth token is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}
