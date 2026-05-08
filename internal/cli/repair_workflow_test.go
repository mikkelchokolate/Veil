package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunRepairWorkflowDryRunPrintsPlanWithoutApply(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := runRepairWorkflow(cmd, repairWorkflowOptions{
		Profile: "ru-recommended",
		Stack:   "panel",
		DryRun:  true,
		EtcDir:  t.TempDir(),
		VarDir:  t.TempDir(),
	})
	if err != nil {
		t.Fatalf("runRepairWorkflow: %v\n%s", err, out.String())
	}
	for _, want := range []string{"Veil repair plan", "repair missing"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
}
