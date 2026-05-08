package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/veil-panel/veil/internal/installer"
	"github.com/veil-panel/veil/internal/service"
)

var installSystemdRunFunc = func(actions []service.SystemdAction) error {
	return service.RunSystemdActions(service.ExecRunner{}, actions)
}

var installExecutableFunc = os.Executable

func applyRURecommendedInstall(cmd *cobra.Command, profile installer.RURecommendedProfile, opts ruRecommendedInstallOptions) error {
	actualBackupDir := opts.BackupDir
	if !opts.BackupDirSet {
		actualBackupDir = filepath.Join(opts.VarDir, "backups")
	}
	systemdDir := opts.SystemdDir
	if systemdDir == "" {
		systemdDir = defaultSystemdDir
	}
	veilBinary, err := installExecutableFunc()
	if err != nil {
		veilBinary = ""
	}
	result, err := installApplyFunc(profile, installer.ApplyPaths{EtcDir: opts.EtcDir, VarDir: opts.VarDir, SystemdDir: systemdDir, BackupDir: actualBackupDir, VeilBinary: veilBinary, CaddyBinary: opts.CaddyBinary})
	if err != nil {
		_ = writeAuditInstall(opts.AuditLog, result.BackupID, false, err.Error(), nil)
		return err
	}
	if err := installSystemdRunFunc(service.SystemdApplyPlan(systemdUnitsForProfile(profile))); err != nil {
		_ = writeAuditInstall(opts.AuditLog, result.BackupID, false, err.Error(), result.WrittenFiles)
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Written files:")
	for _, path := range result.WrittenFiles {
		fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", path)
	}
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprint(cmd.OutOrStdout(), installCredentialSummary(profile))
	if err := writeAuditInstall(opts.AuditLog, result.BackupID, true, "", result.WrittenFiles); err != nil {
		return fmt.Errorf("audit log write failed after successful install: %w", err)
	}
	return nil
}
