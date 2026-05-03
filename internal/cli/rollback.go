package cli

import (
	"fmt"

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
			if backupDir == "" {
				return fmt.Errorf("--backup-dir is required")
			}
			ids, err := installer.ListBackups(backupDir)
			if err != nil {
				return err
			}
			if len(ids) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No backups found")
				return nil
			}
			for _, id := range ids {
				fmt.Fprintln(cmd.OutOrStdout(), id)
			}
			return nil
		},
	}

	restoreCmd := &cobra.Command{
		Use:   "restore <backupID>",
		Short: "Restore files from a backup",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if backupDir == "" {
				return fmt.Errorf("--backup-dir is required")
			}
			if !yes {
				return fmt.Errorf("restore requires --yes to confirm")
			}
			backupID := args[0]
			restored, err := installer.RestoreFromBackup(backupDir, backupID)
			if err != nil {
				writeAuditRestore(auditLog, backupID, false, err.Error(), nil)
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Restored files:")
			for _, path := range restored {
				fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", path)
			}
			if err := writeAuditRestore(auditLog, backupID, true, "", restored); err != nil {
				return fmt.Errorf("audit log write failed after successful restore: %w", err)
			}
			return nil
		},
	}

	cleanupCmd := &cobra.Command{
		Use:   "cleanup <backupID>",
		Short: "Remove a backup after successful restore",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if backupDir == "" {
				return fmt.Errorf("--backup-dir is required")
			}
			if !yes {
				return fmt.Errorf("cleanup requires --yes to confirm")
			}
			backupID := args[0]
			if err := installer.CleanupBackup(backupDir, backupID); err != nil {
				_ = writeAuditCleanup(auditLog, backupID, false, err.Error())
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Backup %s removed\n", backupID)
			if err := writeAuditCleanup(auditLog, backupID, true, ""); err != nil {
				return fmt.Errorf("audit log write failed after successful cleanup: %w", err)
			}
			return nil
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
