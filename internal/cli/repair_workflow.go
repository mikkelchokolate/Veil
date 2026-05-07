package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/veil-panel/veil/internal/installer"
)

type repairWorkflowOptions struct {
	Profile      string
	Stack        string
	Domain       string
	Email        string
	SharedPort   int
	DryRun       bool
	Yes          bool
	EtcDir       string
	VarDir       string
	SystemdDir   string
	BackupDir    string
	BackupDirSet bool
	AuditLog     string
}

func runRepairWorkflow(cmd *cobra.Command, opts repairWorkflowOptions) error {
	if opts.Profile != "ru-recommended" {
		return fmt.Errorf("profile %q is not implemented yet", opts.Profile)
	}
	if opts.Domain == "" {
		return fmt.Errorf("--domain is required for ru-recommended profile")
	}
	if opts.Email == "" {
		return fmt.Errorf("--email is required for ru-recommended profile")
	}
	if opts.SharedPort <= 0 || opts.SharedPort > 65535 {
		return fmt.Errorf("--port is required and must be between 1 and 65535")
	}
	built, err := installer.BuildRURecommendedProfile(installer.RURecommendedInput{
		Domain:       opts.Domain,
		Email:        opts.Email,
		Stack:        installer.Stack(opts.Stack),
		Port:         opts.SharedPort,
		Availability: installer.PortAvailability{TCPBusy: map[int]bool{}, UDPBusy: map[int]bool{}},
		Secret:       randomSecret,
		RandomPort:   func() int { return 31874 },
	})
	if err != nil {
		return err
	}
	plan, err := installer.BuildRepairPlan(built, installer.ApplyPaths{EtcDir: opts.EtcDir, VarDir: opts.VarDir, SystemdDir: opts.SystemdDir})
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Veil repair plan")
	fmt.Fprintln(cmd.OutOrStdout(), plan.Summary())
	if opts.DryRun {
		return nil
	}
	if !opts.Yes {
		return fmt.Errorf("repair apply requires --yes; rerun with --dry-run to preview")
	}

	// Default backup directory if not explicitly set
	actualBackupDir := opts.BackupDir
	if !opts.BackupDirSet {
		actualBackupDir = filepath.Join(opts.VarDir, "backups")
	}

	// Backup existing files before repairing (only on real apply)
	var backupID string
	if actualBackupDir != "" && len(plan.Actions) > 0 {
		paths := make([]string, 0, len(plan.Actions))
		for _, action := range plan.Actions {
			paths = append(paths, action.Path)
		}
		id, err := installer.BackupBeforeApply(paths, actualBackupDir)
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
