package repair

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/installer"
	"github.com/mikkelchokolate/Veil/internal/service"
	"github.com/mikkelchokolate/Veil/internal/veilenv"
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

// TestApplyRepairPlanSkipsSystemdActionsForAStagedUnitDir locks the gate added
// after the #1503 review: units planned into a staging tree are not loaded by
// the host's systemd, so repair writes them but must not try to enable/restart
// them (install skips host preparation for the same directories).
func TestApplyRepairPlanSkipsSystemdActionsForAStagedUnitDir(t *testing.T) {
	stubQUICBuffers(t)
	var calls int
	runSystemd := func([]service.SystemdAction) error {
		calls++
		return nil
	}
	stagingDir := t.TempDir()
	plan := installer.RepairPlan{Actions: []installer.RepairAction{
		{Path: filepath.Join(stagingDir, "veil.service"), Reason: installer.RepairReasonMissing, Content: "unit", Mode: 0o644},
	}}

	var out bytes.Buffer
	if err := ApplyPlan(plan, Options{Yes: true, SystemdDir: stagingDir, VarDir: t.TempDir(), BackupDirSet: true}, &out, ApplyDependencies{RunSystemd: runSystemd}); err != nil {
		t.Fatalf("applyRepairPlan: %v", err)
	}
	if calls != 0 {
		t.Fatalf("repair ran %d systemd action batches for a staging directory", calls)
	}
	if !strings.Contains(out.String(), "Skipping systemd actions") || !strings.Contains(out.String(), stagingDir) {
		t.Fatalf("repair did not say what it skipped:\n%s", out.String())
	}
	if body, err := os.ReadFile(filepath.Join(stagingDir, "veil.service")); err != nil || string(body) != "unit" {
		t.Fatalf("staged unit was not written: %q %v", body, err)
	}
}

// TestApplyRepairPlanRunsSystemdActionsForTheLiveDir keeps the other side of the
// gate: the packaged unit directory still gets the systemctl actions.
func TestApplyRepairPlanRunsSystemdActionsForTheLiveDir(t *testing.T) {
	stubQUICBuffers(t)
	var calls int
	runSystemd := func([]service.SystemdAction) error {
		calls++
		return nil
	}
	plan := installer.RepairPlan{Actions: []installer.RepairAction{
		{Path: filepath.Join(t.TempDir(), "veil.service"), Reason: installer.RepairReasonMissing, Content: "unit", Mode: 0o644},
	}}

	var out bytes.Buffer
	if err := ApplyPlan(plan, Options{Yes: true, SystemdDir: veilenv.DefaultSystemdDir, VarDir: t.TempDir(), BackupDirSet: true}, &out, ApplyDependencies{RunSystemd: runSystemd}); err != nil {
		t.Fatalf("applyRepairPlan: %v", err)
	}
	if calls != 1 {
		t.Fatalf("repair ran %d systemd action batches, want 1", calls)
	}
}

// stubQUICBuffers keeps the UDP-buffer tuning out of tests that run on a host
// where /etc/sysctl.d is not writable.
func stubQUICBuffers(t *testing.T) {
	t.Helper()
	original := applyQUICBuffers
	applyQUICBuffers = func() error { return nil }
	t.Cleanup(func() { applyQUICBuffers = original })
}

// TestApplyRepairPlanRunsSystemdActionsForAnEmptyProgrammaticDir pins the
// documented compromise: a caller that did not resolve a unit directory keeps
// the historical behaviour, while a resolved staging tree is skipped.
func TestApplyRepairPlanRunsSystemdActionsForAnEmptyProgrammaticDir(t *testing.T) {
	stubQUICBuffers(t)
	var calls int
	plan := installer.RepairPlan{Actions: []installer.RepairAction{
		{Path: filepath.Join(t.TempDir(), "veil.service"), Reason: installer.RepairReasonMissing, Content: "unit", Mode: 0o644},
	}}
	var out bytes.Buffer
	if err := ApplyPlan(plan, Options{Yes: true, VarDir: t.TempDir(), BackupDirSet: true}, &out, ApplyDependencies{RunSystemd: func([]service.SystemdAction) error { calls++; return nil }}); err != nil {
		t.Fatalf("applyRepairPlan: %v", err)
	}
	if calls != 1 {
		t.Fatalf("systemd actions ran %d times for an unresolved directory, want 1", calls)
	}
	if strings.Contains(out.String(), "Skipping systemd actions") {
		t.Fatalf("an unresolved directory must not be reported as staging:\n%s", out.String())
	}
}
