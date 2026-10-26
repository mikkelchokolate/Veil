package cli

import (
	"bytes"
	"strings"
	"testing"

	repairflow "github.com/mikkelchokolate/Veil/internal/cliflow/repair"
	"github.com/mikkelchokolate/Veil/internal/installer"
)

// TestRepairWorkflowPrintsPlanBuildWarnings locks the CLI wiring of #1500: a
// finding reported while the plan is built (a failed certificate renewal, an
// unreproducible Caddy configuration) must reach the operator next to the plan,
// including on a dry run.
func TestRepairWorkflowPrintsPlanBuildWarnings(t *testing.T) {
	oldBuild := repairBuildPlan
	repairBuildPlan = func(_ repairflow.Options, deps repairflow.PlanDependencies) (installer.RepairPlan, error) {
		deps.Notify("could not renew the Let's Encrypt IP certificate: permission denied; the certificate currently at /etc/veil/panel/tls.crt is kept unchanged")
		return installer.RepairPlan{}, nil
	}
	t.Cleanup(func() { repairBuildPlan = oldBuild })

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := runRepairWorkflow(cmd, repairWorkflowOptions{Profile: "ru-recommended", DryRun: true, EtcDir: t.TempDir(), VarDir: t.TempDir()}); err != nil {
		t.Fatalf("runRepairWorkflow: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "warning: could not renew the Let's Encrypt IP certificate") {
		t.Fatalf("repair did not print the plan-building warning:\n%s", got)
	}
	if strings.Index(got, "warning:") < strings.Index(got, "Veil repair plan") {
		t.Fatalf("warning must follow the plan:\n%s", got)
	}
}
