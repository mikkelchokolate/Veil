package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/veil-panel/veil/internal/installer"
	"github.com/veil-panel/veil/internal/service"
)

func TestApplyRepairPlanRunsSystemdActionsForRepairedUnits(t *testing.T) {
	oldSystemd := installSystemdRunFunc
	var gotActions []service.SystemdAction
	installSystemdRunFunc = func(actions []service.SystemdAction) error {
		gotActions = actions
		return nil
	}
	t.Cleanup(func() { installSystemdRunFunc = oldSystemd })

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	plan := installer.RepairPlan{Actions: []installer.RepairAction{{Path: t.TempDir() + "/veil.service", Reason: installer.RepairReasonMissing, Content: "unit", Mode: 0o644}}}
	if err := applyRepairPlan(cmd, plan, repairWorkflowOptions{Yes: true, VarDir: t.TempDir(), BackupDirSet: true}); err != nil {
		t.Fatalf("applyRepairPlan: %v", err)
	}
	if len(gotActions) == 0 || gotActions[0].Command != "systemctl" || gotActions[0].Args[0] != "daemon-reload" {
		t.Fatalf("systemd actions not run for repaired units: %+v", gotActions)
	}
}

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
