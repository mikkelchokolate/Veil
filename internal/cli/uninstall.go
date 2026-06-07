package cli

import (
	uninstallflow "github.com/mikkelchokolate/Veil/internal/cliflow/uninstall"
	"github.com/spf13/cobra"
)

var uninstallServiceStopper = uninstallflow.StopAndDisableService
var uninstallFileRemover = uninstallflow.RemovePath
var uninstallSystemdReloader = uninstallflow.ReloadSystemdDaemon

func newUninstallCommand() *cobra.Command {
	var dryRun bool
	var yes bool
	var purge bool
	var keepData bool
	var etcDir string
	var varDir string
	var systemdDir string
	var installDir string

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove Veil panel, services, configuration, and state",
		Long: "Remove the Veil panel, services, configuration, and state.\n\n" +
			"By default this also removes the encrypted state and credentials in /etc/veil and\n" +
			"/var/lib/veil, so a later `veil install` starts fresh with a new password and panel\n" +
			"path. Pass --keep-data to preserve configuration and credentials across reinstalls.\n" +
			"The veil system account is always preserved.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return uninstallflow.Run(uninstallflow.Options{DryRun: dryRun, Yes: yes, Purge: purge, KeepData: keepData, EtcDir: etcDir, VarDir: varDir, SystemdDir: systemdDir, InstallDir: installDir}, cmd.OutOrStdout(), cmd.ErrOrStderr(), uninstallflow.Dependencies{
				ServiceStopper:  uninstallServiceStopper,
				FileRemover:     uninstallFileRemover,
				SystemdReloader: uninstallSystemdReloader,
			})
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print uninstall plan without removing anything")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm uninstall operation")
	cmd.Flags().BoolVar(&keepData, "keep-data", false, "preserve Veil configuration and state so a reinstall reuses the existing credentials and panel path")
	cmd.Flags().BoolVar(&purge, "purge", false, "remove Veil configuration and state (now the default; kept for compatibility); always wins over --keep-data")
	cmd.Flags().StringVar(&etcDir, "etc-dir", "/etc/veil", "Veil configuration directory")
	cmd.Flags().StringVar(&varDir, "var-dir", "/var/lib/veil", "Veil state directory")
	cmd.Flags().StringVar(&systemdDir, "systemd-dir", defaultSystemdDir, "systemd unit directory to remove Veil units from")
	cmd.Flags().StringVar(&installDir, "install-dir", "/usr/local/bin", "directory containing the veil binary")

	return cmd
}
