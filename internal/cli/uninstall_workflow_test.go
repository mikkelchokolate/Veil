package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestUninstallWorkflowDryRunPrintsPlanWithoutRemoving(t *testing.T) {
	var out bytes.Buffer
	called := false
	workflow := NewUninstallWorkflow(uninstallWorkflowOptions{DryRun: true}, &out, &bytes.Buffer{})
	workflow.serviceStopper = func(string) error { called = true; return nil }
	workflow.fileRemover = func(string) error { called = true; return nil }

	if err := workflow.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if called {
		t.Fatalf("dry run should not stop services or remove files")
	}
	if !strings.Contains(out.String(), "Veil uninstall plan") || !strings.Contains(out.String(), "veil.service") {
		t.Fatalf("output = %s", out.String())
	}
}
