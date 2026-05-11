package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/veil-panel/veil/internal/backup"
	"github.com/veil-panel/veil/internal/installer"
	"github.com/veil-panel/veil/internal/service"
)

func applyRepairPlan(cmd *cobra.Command, plan installer.RepairPlan, opts repairWorkflowOptions) error {
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
	if actions := service.SystemdApplyPlan(systemdUnitsFromRepairPlan(plan)); len(actions) > 0 {
		if err := installSystemdRunFunc(actions); err != nil {
			_ = writeAuditRepair(opts.AuditLog, backupID, false, err.Error(), result.WrittenFiles)
			return err
		}
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Repaired files:")
	for _, path := range result.WrittenFiles {
		fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", path)
	}
	if backupID != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Backup ID: %s\n", backupID)
	} else if len(plan.Actions) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No backup created")
	}
	if err := writeAuditRepair(opts.AuditLog, backupID, true, "", result.WrittenFiles); err != nil {
		return fmt.Errorf("audit log write failed after successful repair: %w", err)
	}
	return nil
}

func systemdUnitsFromRepairPlan(plan installer.RepairPlan) []string {
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
