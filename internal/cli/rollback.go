package cli

import (
	"github.com/spf13/cobra"
	"github.com/veil-panel/veil/internal/installer"
)

func newRollbackCommand() *cobra.Command {
	var backupDir string
	var yes bool
	var auditLog string

	cmd := &cobra.Command{
		Use:   "rollback",
		Short: "Manage backups of configuration files",
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List available backups",
		RunE: func(cmd *cobra.Command, args []string) error {
			return NewRollbackWorkflow(rollbackWorkflowOptions{BackupDir: backupDir, Yes: yes, AuditLog: auditLog}, cmd.OutOrStdout()).List()
		},
	}

	restoreCmd := &cobra.Command{
		Use:   "restore <backupID>",
		Short: "Restore files from a backup",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return NewRollbackWorkflow(rollbackWorkflowOptions{BackupDir: backupDir, Yes: yes, AuditLog: auditLog}, cmd.OutOrStdout()).Restore(args[0])
		},
	}

	cleanupCmd := &cobra.Command{
		Use:   "cleanup <backupID>",
		Short: "Remove a backup after successful restore",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return NewRollbackWorkflow(rollbackWorkflowOptions{BackupDir: backupDir, Yes: yes, AuditLog: auditLog}, cmd.OutOrStdout()).Cleanup(args[0])
		},
	}

	cmd.AddCommand(listCmd, restoreCmd, cleanupCmd)

	cmd.PersistentFlags().StringVar(&backupDir, "backup-dir", "", "backup directory (required)")
	cmd.PersistentFlags().BoolVar(&yes, "yes", false, "confirm restore/cleanup operation")
	cmd.PersistentFlags().StringVar(&auditLog, "audit-log", "", "optional path for JSONL audit log")

	return cmd
}

func writeAuditRestore(auditLog, backupID string, success bool, errMsg string, restoredFiles []string) error {
	return installer.AppendAuditEvent(auditLog, installer.AuditEvent{
		Action:        "rollback.restore",
		BackupID:      backupID,
		Success:       success,
		Error:         errMsg,
		RestoredFiles: restoredFiles,
	})
}

func writeAuditCleanup(auditLog, backupID string, success bool, errMsg string) error {
	return installer.AppendAuditEvent(auditLog, installer.AuditEvent{
		Action:   "rollback.cleanup",
		BackupID: backupID,
		Success:  success,
		Error:    errMsg,
	})
}
