package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunServeWorkflowRejectsNonLoopbackWithoutAuth(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := runServeWorkflow(cmd, serveWorkflowOptions{
		Version:   "test",
		Listen:    "0.0.0.0:2096",
		StatePath: filepath.Join(t.TempDir(), "state.json"),
	})
	if err == nil {
		t.Fatalf("expected auth binding error")
	}
	if !strings.Contains(err.Error(), "public Panel listen") || !strings.Contains(err.Error(), "veil admin reset") {
		t.Fatalf("unexpected error: %v", err)
	}
}
