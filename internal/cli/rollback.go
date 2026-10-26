package cli

import (
	"path/filepath"
	"strings"

	rollbackflow "github.com/mikkelchokolate/Veil/internal/cliflow/rollback"
	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/spf13/cobra"
)

// rollbackDefaultSystemdDir is the unit directory install and repair back up;
// it must stay absolute. Tests replace it (an empty value disables the name
// filter), and a custom --systemd-dir install still needs an explicit
// --restore-root for its own tree.
var rollbackDefaultSystemdDir = defaultSystemdDir

func rollbackSystemdRoot() string { return rollbackDefaultSystemdDir }

// rollbackRestorePathFilter narrows the restore allowlist inside systemdRoot to
// the unit and drop-in names Veil manages: veil*.service|socket|timer, their
// <unit>.d/*.conf drop-ins, and legacy veil-caddy@* instances. Paths outside
// systemdRoot are left to the root allowlist.
func rollbackRestorePathFilter(systemdRoot string) func(string) bool {
	root := filepath.Clean(systemdRoot)
	return func(path string) bool {
		rel, err := filepath.Rel(root, filepath.Clean(path))
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			return true
		}
		parts := strings.Split(rel, string(filepath.Separator))
		switch len(parts) {
		case 1:
			name := parts[0]
			return strings.HasPrefix(name, "veil") &&
				(strings.HasSuffix(name, ".service") || strings.HasSuffix(name, ".socket") || strings.HasSuffix(name, ".timer"))
		case 2:
			dir, file := parts[0], parts[1]
			return strings.HasPrefix(dir, "veil") && strings.HasSuffix(dir, ".d") && strings.HasSuffix(file, ".conf")
		default:
			return false
		}
	}
}

func newRollbackCommand() *cobra.Command {
	var backupDir string
	var yes bool
	var auditLog string
	var restoreRoots []string

	// The systemd unit directory is in the defaults because install and repair
	// back up the units they overwrite (#1446), but a manifest is data on disk,
	// not authority: inside that root only Veil's own units and drop-ins may be
	// written, or a forged manifest would become root-executed code (#1219).
	restoreFilter := rollbackRestorePathFilter(rollbackSystemdRoot())
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
		return rollbackflow.NewWorkflow(rollbackflow.Options{BackupDir: backupDir, Yes: yes, AuditLog: auditLog, RestoreRoots: roots, RestorePathFilter: restoreFilter}, cmd.OutOrStdout())
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
