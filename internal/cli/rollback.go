package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/veil-panel/veil/internal/installer"
)

func newRollbackCommand() *cobra.Command {
	var backupDir string
	var yes bool

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
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Restored files:")
			for _, path := range restored {
				fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", path)
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
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Backup %s removed\n", backupID)
			return nil
		},
	}

	cmd.AddCommand(listCmd, restoreCmd, cleanupCmd)

	cmd.PersistentFlags().StringVar(&backupDir, "backup-dir", "", "backup directory (required)")
	cmd.PersistentFlags().BoolVar(&yes, "yes", false, "confirm restore/cleanup operation")

	return cmd
}
