package cli

import (
	"github.com/spf13/cobra"
	"github.com/veil-panel/veil/internal/audit"
)

func newRepairCommand() *cobra.Command {
	var profile string
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
			return runRepairWorkflow(cmd, repairWorkflowOptions{
				Profile:      profile,
				DryRun:       dryRun,
				Yes:          yes,
				EtcDir:       etcDir,
				VarDir:       varDir,
				SystemdDir:   systemdDir,
				BackupDir:    backupDir,
				BackupDirSet: cmd.Flags().Changed("backup-dir"),
				AuditLog:     auditLog,
			})
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "ru-recommended", "repair profile: ru-recommended")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print repair plan without writing files")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm repairing planned files")
	cmd.Flags().StringVar(&etcDir, "etc-dir", "/etc/veil", "Veil configuration directory")
	cmd.Flags().StringVar(&varDir, "var-dir", "/var/lib/veil", "Veil state directory")
	cmd.Flags().StringVar(&systemdDir, "systemd-dir", defaultSystemdDir, "systemd unit output directory")
	cmd.Flags().StringVar(&backupDir, "backup-dir", "", "backup directory for files before overwrite (optional; defaults to var-dir/backups; pass empty string to disable)")
	cmd.Flags().StringVar(&auditLog, "audit-log", "", "optional path for JSONL audit log")
	return cmd
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
