package repair

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/installer"
	"github.com/mikkelchokolate/Veil/internal/service"
)

func TestApplyRepairPlanRequiresYes(t *testing.T) {
	var out bytes.Buffer
	err := ApplyPlan(installer.RepairPlan{}, Options{}, &out, ApplyDependencies{})
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("expected --yes error, got %v", err)
	}
}

func TestApplyRepairPlanBackupFailure(t *testing.T) {
	var out bytes.Buffer
	varDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(varDir, "backups"), []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("create backup-dir blocker: %v", err)
	}
	plan := installer.RepairPlan{Actions: []installer.RepairAction{{Path: filepath.Join(t.TempDir(), "veil.env"), Reason: installer.RepairReasonMissing, Content: "x", Mode: 0o600}}}

	err := ApplyPlan(plan, Options{Yes: true, VarDir: varDir}, &out, ApplyDependencies{})
	if err == nil {
		t.Fatal("expected backup error")
	}
}

func TestApplyRepairPlanApplyFailure(t *testing.T) {
	var out bytes.Buffer
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "blocked"), []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("create blocker: %v", err)
	}
	plan := installer.RepairPlan{Actions: []installer.RepairAction{{Path: filepath.Join(tmp, "blocked", "x"), Reason: installer.RepairReasonMissing, Content: "x", Mode: 0o600}}}

	err := ApplyPlan(plan, Options{Yes: true, BackupDirSet: true, BackupDir: tmp}, &out, ApplyDependencies{})
	if err == nil {
		t.Fatal("expected apply error")
	}
}

func TestApplyRepairPlanSystemdRunnerMissing(t *testing.T) {
	var out bytes.Buffer
	tmp := t.TempDir()
	plan := installer.RepairPlan{Actions: []installer.RepairAction{{Path: filepath.Join(tmp, "veil-mieru.service"), Reason: installer.RepairReasonMissing, Content: "unit", Mode: 0o644}}}

	err := ApplyPlan(plan, Options{Yes: true, BackupDirSet: true, BackupDir: tmp}, &out, ApplyDependencies{})
	if err == nil || !strings.Contains(err.Error(), "systemd runner is not configured") {
		t.Fatalf("expected systemd runner missing error, got %v", err)
	}
}

func TestApplyRepairPlanSystemdRunError(t *testing.T) {
	var out bytes.Buffer
	tmp := t.TempDir()
	wantErr := errors.New("systemctl failed")
	plan := installer.RepairPlan{Actions: []installer.RepairAction{{Path: filepath.Join(tmp, "veil-mieru.service"), Reason: installer.RepairReasonMissing, Content: "unit", Mode: 0o644}}}

	err := ApplyPlan(plan, Options{Yes: true, BackupDirSet: true, BackupDir: tmp}, &out, ApplyDependencies{RunSystemd: func([]service.SystemdAction) error { return wantErr }})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected systemctl error, got %v", err)
	}
}

func TestApplyRepairPlanAuditWriteError(t *testing.T) {
	var out bytes.Buffer
	tmp := t.TempDir()
	plan := installer.RepairPlan{Actions: []installer.RepairAction{{Path: filepath.Join(tmp, "veil.env"), Reason: installer.RepairReasonMissing, Content: "x", Mode: 0o600}}}

	err := ApplyPlan(plan, Options{Yes: true, BackupDirSet: true, BackupDir: tmp, AuditLog: tmp}, &out, ApplyDependencies{})
	if err == nil || !strings.Contains(err.Error(), "audit log write failed after successful repair") {
		t.Fatalf("expected audit write error, got %v", err)
	}
}

func TestApplyRepairPlanNoBackupWhenBackupDirEmpty(t *testing.T) {
	var out bytes.Buffer
	tmp := t.TempDir()
	plan := installer.RepairPlan{Actions: []installer.RepairAction{{Path: filepath.Join(tmp, "veil.env"), Reason: installer.RepairReasonMissing, Content: "x", Mode: 0o600}}}

	if err := ApplyPlan(plan, Options{Yes: true, BackupDirSet: true}, &out, ApplyDependencies{}); err != nil {
		t.Fatalf("ApplyPlan: %v", err)
	}
	if strings.Contains(out.String(), "Backup ID:") {
		t.Fatalf("expected no backup ID when BackupDir is empty, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "Repaired files:") {
		t.Fatalf("expected repaired files output, got:\n%s", out.String())
	}
}

func TestApplyRepairPlanPrintsBackupID(t *testing.T) {
	var out bytes.Buffer
	tmp := t.TempDir()
	path := filepath.Join(tmp, "veil.env")
	plan := installer.RepairPlan{Actions: []installer.RepairAction{{Path: path, Reason: installer.RepairReasonMissing, Content: "x", Mode: 0o600}}}

	if err := ApplyPlan(plan, Options{Yes: true, VarDir: tmp}, &out, ApplyDependencies{}); err != nil {
		t.Fatalf("ApplyPlan: %v", err)
	}
	if !strings.Contains(out.String(), "Backup ID:") {
		t.Fatalf("expected backup ID output, got:\n%s", out.String())
	}
}

func TestSystemdUnitsFromRepairPlanDeduplicates(t *testing.T) {
	plan := installer.RepairPlan{Actions: []installer.RepairAction{
		{Path: "/etc/systemd/system/veil-mieru.service"},
		{Path: "/etc/systemd/system/veil-mieru.service"},
		{Path: "/etc/veil/veil.env"},
	}}
	units := SystemdUnitsFromRepairPlan(plan)
	if len(units) != 1 || units[0] != "veil-mieru.service" {
		t.Fatalf("expected one unit, got %v", units)
	}
}
