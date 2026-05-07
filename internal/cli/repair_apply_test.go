package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/veil-panel/veil/internal/installer"
)

func TestApplyRepairPlanReportsNoBackupWhenNoActions(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := applyRepairPlan(cmd, installer.RepairPlan{}, repairWorkflowOptions{Yes: true, VarDir: t.TempDir()})
	if err != nil {
		t.Fatalf("applyRepairPlan: %v", err)
	}
	for _, want := range []string{"Repaired files:", "No backup created"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
}
