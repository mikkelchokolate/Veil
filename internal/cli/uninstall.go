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
	var etcDir string
	var varDir string
	var systemdDir string
	var installDir string

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove Veil panel, services, and configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			return uninstallflow.Run(uninstallflow.Options{DryRun: dryRun, Yes: yes, Purge: purge, EtcDir: etcDir, VarDir: varDir, SystemdDir: systemdDir, InstallDir: installDir}, cmd.OutOrStdout(), cmd.ErrOrStderr(), uninstallflow.Dependencies{
				ServiceStopper:  uninstallServiceStopper,
				FileRemover:     uninstallFileRemover,
				SystemdReloader: uninstallSystemdReloader,
			})
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print uninstall plan without removing anything")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm uninstall operation")
	cmd.Flags().BoolVar(&purge, "purge", false, "also remove Veil configuration and state; the veil system account is preserved")
	cmd.Flags().StringVar(&etcDir, "etc-dir", "/etc/veil", "Veil configuration directory")
	cmd.Flags().StringVar(&varDir, "var-dir", "/var/lib/veil", "Veil state directory")
	cmd.Flags().StringVar(&systemdDir, "systemd-dir", defaultSystemdDir, "systemd unit directory to remove Veil units from")
	cmd.Flags().StringVar(&installDir, "install-dir", "/usr/local/bin", "directory containing the veil binary")

	return cmd
}
