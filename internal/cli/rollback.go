package cli

import (
	"strings"

	rollbackflow "github.com/mikkelchokolate/Veil/internal/cliflow/rollback"
	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/spf13/cobra"
)

// rollbackDefaultSystemdDir is the unit directory install and repair back up.
// Tests replace it, and a custom --systemd-dir install still needs an explicit
// --restore-root for its own tree.
var rollbackDefaultSystemdDir = defaultSystemdDir

func rollbackSystemdRoot() string {
	if strings.TrimSpace(rollbackDefaultSystemdDir) == "" {
		return defaultSystemdDir
	}
	return rollbackDefaultSystemdDir
}

func newRollbackCommand() *cobra.Command {
	var backupDir string
	var yes bool
	var auditLog string
	var restoreRoots []string

	// Rollback destinations are constrained to the managed roots the backups
	// were taken from — the manifest is on-disk data, not authority (#1219).
	// The systemd unit directory is part of that set: install and repair back
	// up every unit and drop-in they are about to overwrite, so omitting it
	// refused every backup the product itself writes (#1446).
	defaultRestoreRoots := []string{hostenv.EtcDir(), hostenv.VarDir(), rollbackSystemdRoot()}

	workflow := func(cmd *cobra.Command) rollbackflow.Workflow {
		roots := restoreRoots
		if len(roots) == 0 {
			roots = defaultRestoreRoots
		}
		return rollbackflow.NewWorkflow(rollbackflow.Options{BackupDir: backupDir, Yes: yes, AuditLog: auditLog, RestoreRoots: roots}, cmd.OutOrStdout())
	}

	cmd := &cobra.Command{
		Use:   "rollback",
		Short: "Manage backups of configuration files",
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List available backups",
		RunE: func(cmd *cobra.Command, args []string) error {
			return workflow(cmd).List()
		},
	}

	restoreCmd := &cobra.Command{
		Use:   "restore <backupID>",
		Short: "Restore files from a backup",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return workflow(cmd).Restore(args[0])
		},
	}

	cleanupCmd := &cobra.Command{
		Use:   "cleanup <backupID>",
		Short: "Remove a backup after successful restore",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return workflow(cmd).Cleanup(args[0])
		},
	}

	cmd.AddCommand(listCmd, restoreCmd, cleanupCmd)

	cmd.PersistentFlags().StringVar(&backupDir, "backup-dir", "", "backup directory (required)")
	cmd.PersistentFlags().BoolVar(&yes, "yes", false, "confirm restore/cleanup operation")
	cmd.PersistentFlags().StringVar(&auditLog, "audit-log", "", "optional path for JSONL audit log")
	restoreCmd.Flags().StringArrayVar(&restoreRoots, "restore-root", nil, "allowed restore destination root (repeatable); defaults to the managed etc, var and systemd directories")

	return cmd
}
