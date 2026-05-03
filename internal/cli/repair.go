package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/veil-panel/veil/internal/installer"
)

func newRepairCommand() *cobra.Command {
	var profile string
	var stack string
	var domain string
	var email string
	var sharedPort int
	var dryRun bool
	var yes bool
	var etcDir string
	var varDir string
	var systemdDir string
	var backupDir string
	var auditLog string

	cmd := &cobra.Command{
		Use:   "repair",
		Short: "Repair Veil managed generated files without arbitrary side effects",
		RunE: func(cmd *cobra.Command, args []string) error {
			if profile != "ru-recommended" {
				return fmt.Errorf("profile %q is not implemented yet", profile)
			}
			if domain == "" {
				return fmt.Errorf("--domain is required for ru-recommended profile")
			}
			if email == "" {
				return fmt.Errorf("--email is required for ru-recommended profile")
			}
			if sharedPort <= 0 || sharedPort > 65535 {
				return fmt.Errorf("--port is required and must be between 1 and 65535")
			}
			built, err := installer.BuildRURecommendedProfile(installer.RURecommendedInput{
				Domain:       domain,
				Email:        email,
				Stack:        installer.Stack(stack),
				Port:         sharedPort,
				Availability: installer.PortAvailability{TCPBusy: map[int]bool{}, UDPBusy: map[int]bool{}},
				Secret:       randomSecret,
				RandomPort:   func() int { return 31874 },
			})
			if err != nil {
				return err
			}
			plan, err := installer.BuildRepairPlan(built, installer.ApplyPaths{EtcDir: etcDir, VarDir: varDir, SystemdDir: systemdDir})
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Veil repair plan")
			fmt.Fprintln(cmd.OutOrStdout(), plan.Summary())
			if dryRun {
				return nil
			}
			if !yes {
				return fmt.Errorf("repair apply requires --yes; rerun with --dry-run to preview")
			}

			// Backup existing files before repairing (only on real apply)
			var backupID string
			if backupDir != "" && len(plan.Actions) > 0 {
				paths := make([]string, 0, len(plan.Actions))
				for _, action := range plan.Actions {
					paths = append(paths, action.Path)
				}
				id, err := installer.BackupBeforeApply(paths, backupDir)
				if err != nil {
					_ = writeAuditRepair(auditLog, "", false, err.Error(), nil)
					return err
				}
				backupID = id
			}

			result, err := installer.ApplyRepairPlan(plan)
			if err != nil {
				_ = writeAuditRepair(auditLog, backupID, false, err.Error(), nil)
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
			if err := writeAuditRepair(auditLog, backupID, true, "", result.WrittenFiles); err != nil {
				return fmt.Errorf("audit log write failed after successful repair: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "default", "repair profile: default or ru-recommended")
	cmd.Flags().StringVar(&stack, "stack", "both", "proxy stack to repair: both, naive, or hysteria2")
	cmd.Flags().StringVar(&domain, "domain", "", "domain for regenerated managed files")
	cmd.Flags().StringVar(&email, "email", "", "ACME email")
	cmd.Flags().IntVar(&sharedPort, "port", 0, "required shared proxy port for NaiveProxy TCP and/or Hysteria2 UDP")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print repair plan without writing files")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm repairing planned files")
	cmd.Flags().StringVar(&etcDir, "etc-dir", "/etc/veil", "Veil configuration directory")
	cmd.Flags().StringVar(&varDir, "var-dir", "/var/lib/veil", "Veil state directory")
	cmd.Flags().StringVar(&systemdDir, "systemd-dir", "", "optional systemd unit output directory, e.g. /etc/systemd/system")
	cmd.Flags().StringVar(&backupDir, "backup-dir", "", "backup directory for files before repair (optional)")
	cmd.Flags().StringVar(&auditLog, "audit-log", "", "optional path for JSONL audit log")
	return cmd
}

func writeAuditRepair(auditLog, backupID string, success bool, errMsg string, writtenFiles []string) error {
	return installer.AppendAuditEvent(auditLog, installer.AuditEvent{
		Action:       "repair.apply",
		BackupID:     backupID,
		Success:      success,
		Error:        errMsg,
		WrittenFiles: writtenFiles,
	})
}
