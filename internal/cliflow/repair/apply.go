package repair

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

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
	if actions := SystemdActionsFromRepairPlan(plan); len(actions) > 0 {
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

func SystemdActionsFromRepairPlan(plan installer.RepairPlan) []service.SystemdAction {
	dirs := map[string]string{}
	var names []string
	for _, action := range plan.Actions {
		name := filepath.Base(action.Path)
		switch filepath.Ext(name) {
		case ".service", ".socket", ".timer":
		default:
			continue
		}
		if _, exists := dirs[name]; exists {
			continue
		}
		dirs[name] = filepath.Dir(action.Path)
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil
	}

	enableNow := map[string]struct{}{}
	enable := map[string]struct{}{}
	restart := map[string]struct{}{}
	addStartable := func(name string) {
		if isSystemdTemplate(name) {
			return
		}
		enable[name] = struct{}{}
		restart[name] = struct{}{}
	}
	for _, name := range names {
		switch name {
		case "veil-helper.service", "veil-helper.socket":
			enableNow["veil-helper.socket"] = struct{}{}
			continue
		case "veil-backup.service", "veil-backup.timer":
			enableNow["veil-backup.timer"] = struct{}{}
			continue
		}
		if strings.HasSuffix(name, ".socket") || strings.HasSuffix(name, ".timer") {
			enableNow[name] = struct{}{}
			continue
		}
		if isSystemdTemplate(name) {
			prefix := strings.TrimSuffix(name, ".service")
			matches, _ := filepath.Glob(filepath.Join(dirs[name], prefix+"*.service"))
			for _, match := range matches {
				instance := filepath.Base(match)
				if !isSystemdTemplate(instance) {
					addStartable(instance)
				}
			}
			continue
		}
		if strings.HasSuffix(name, ".service") {
			addStartable(name)
		}
	}

	actions := []service.SystemdAction{{Command: "systemctl", Args: []string{"daemon-reload"}}}
	for _, name := range sortedUnitNames(enableNow) {
		actions = append(actions, service.SystemdAction{Command: "systemctl", Args: []string{"enable", "--now", name}})
	}
	for _, name := range sortedUnitNames(enable) {
		if _, ok := enableNow[name]; ok {
			continue
		}
		actions = append(actions, service.SystemdAction{Command: "systemctl", Args: []string{"enable", name}})
	}
	for _, name := range sortedUnitNames(restart) {
		actions = append(actions, service.SystemdAction{Command: "systemctl", Args: []string{"restart", name}})
	}
	return actions
}

func isSystemdTemplate(name string) bool {
	return strings.Contains(name, "@.")
}

func sortedUnitNames(set map[string]struct{}) []string {
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
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
