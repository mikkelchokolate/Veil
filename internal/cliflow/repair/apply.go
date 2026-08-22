package repair

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/mikkelchokolate/Veil/internal/audit"
	"github.com/mikkelchokolate/Veil/internal/backup"
	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/mikkelchokolate/Veil/internal/installer"
	"github.com/mikkelchokolate/Veil/internal/service"
)

type ApplyDependencies struct {
	RunSystemd func([]service.SystemdAction) error
}

func ApplyPlan(plan installer.RepairPlan, opts Options, out io.Writer, deps ApplyDependencies) error {
	if !opts.Yes {
		return fmt.Errorf("repair apply requires --yes; rerun with --dry-run to preview")
	}
	actualBackupDir := opts.BackupDir
	if !opts.BackupDirSet {
		actualBackupDir = filepath.Join(opts.VarDir, "backups")
	}
	var backupID string
	if actualBackupDir != "" && len(plan.Actions) > 0 {
		paths := make([]string, 0, len(plan.Actions))
		for _, action := range plan.Actions {
			paths = append(paths, action.Path)
		}
		id, err := backup.NewLifecycle(actualBackupDir).BackupExisting(paths)
		if err != nil {
			_ = writeAuditRepair(opts.AuditLog, "", false, err.Error(), nil)
			return err
		}
		backupID = id
	}
	result, err := installer.ApplyRepairPlan(plan)
	if err != nil {
		_ = writeAuditRepair(opts.AuditLog, backupID, false, err.Error(), nil)
		return err
	}
	if err := hostenv.ApplyQUICUDPBuffers(); err != nil {
		_ = writeAuditRepair(opts.AuditLog, backupID, false, err.Error(), result.WrittenFiles)
		return fmt.Errorf("tune QUIC UDP buffers: %w", err)
	}
	if actions := service.SystemdApplyPlan(SystemdUnitsFromRepairPlan(plan)); len(actions) > 0 {
		if deps.RunSystemd == nil {
			return fmt.Errorf("repair systemd runner is not configured")
		}
		if err := deps.RunSystemd(actions); err != nil {
			_ = writeAuditRepair(opts.AuditLog, backupID, false, err.Error(), result.WrittenFiles)
			return err
		}
	}
	fmt.Fprintln(out, "Repaired files:")
	for _, path := range result.WrittenFiles {
		fmt.Fprintf(out, "- %s\n", path)
	}
	if backupID != "" {
		fmt.Fprintf(out, "Backup ID: %s\n", backupID)
	} else if len(plan.Actions) == 0 {
		fmt.Fprintln(out, "No backup created")
	}
	if err := writeAuditRepair(opts.AuditLog, backupID, true, "", result.WrittenFiles); err != nil {
		return fmt.Errorf("audit log write failed after successful repair: %w", err)
	}
	return nil
}

func SystemdUnitsFromRepairPlan(plan installer.RepairPlan) []string {
	seen := map[string]bool{}
	units := []string{}
	for _, action := range plan.Actions {
		name := filepath.Base(action.Path)
		if filepath.Ext(name) != ".service" || seen[name] {
			continue
		}
		seen[name] = true
		units = append(units, name)
	}
	return units
}

func writeAuditRepair(auditLog, backupID string, success bool, errMsg string, writtenFiles []string) error {
	return audit.AppendAuditEvent(auditLog, audit.AuditEvent{
		Action:       "repair.apply",
		BackupID:     backupID,
		Success:      success,
		Error:        errMsg,
		WrittenFiles: writtenFiles,
	})
}
