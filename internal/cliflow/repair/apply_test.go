package repair

import (
	"bytes"
	"strings"
	"testing"

	"github.com/veil-panel/veil/internal/installer"
	"github.com/veil-panel/veil/internal/service"
)

func TestApplyRepairPlanRunsSystemdActionsForRepairedUnits(t *testing.T) {
	var gotActions []service.SystemdAction
	installSystemdRunFunc := func(actions []service.SystemdAction) error {
		gotActions = actions
		return nil
	}

	var out bytes.Buffer
	plan := installer.RepairPlan{Actions: []installer.RepairAction{{Path: t.TempDir() + "/veil.service", Reason: installer.RepairReasonMissing, Content: "unit", Mode: 0o644}}}
	if err := ApplyPlan(plan, Options{Yes: true, VarDir: t.TempDir(), BackupDirSet: true}, &out, ApplyDependencies{RunSystemd: installSystemdRunFunc}); err != nil {
		t.Fatalf("applyRepairPlan: %v", err)
	}
	if len(gotActions) == 0 || gotActions[0].Command != "systemctl" || gotActions[0].Args[0] != "daemon-reload" {
		t.Fatalf("systemd actions not run for repaired units: %+v", gotActions)
	}
}

func TestApplyRepairPlanReportsNoBackupWhenNoActions(t *testing.T) {
	var out bytes.Buffer

	err := ApplyPlan(installer.RepairPlan{}, Options{Yes: true, VarDir: t.TempDir()}, &out, ApplyDependencies{})
	if err != nil {
		t.Fatalf("applyRepairPlan: %v", err)
	}
	for _, want := range []string{"Repaired files:", "No backup created"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
}
