package cli

import (
	"github.com/spf13/cobra"
	uninstallflow "github.com/veil-panel/veil/internal/cliflow/uninstall"
)

var uninstallServiceStopper = uninstallflow.StopAndDisableService
var uninstallFileRemover = uninstallflow.RemovePath
var uninstallSystemdReloader = uninstallflow.ReloadSystemdDaemon

func newUninstallCommand() *cobra.Command {
	var dryRun bool
	var yes bool
	var etcDir string
	var varDir string
	var systemdDir string
	var installDir string

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove Veil panel, services, and configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			return NewUninstallWorkflow(uninstallWorkflowOptions{DryRun: dryRun, Yes: yes, EtcDir: etcDir, VarDir: varDir, SystemdDir: systemdDir, InstallDir: installDir}, cmd.OutOrStdout(), cmd.ErrOrStderr()).Run()
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print uninstall plan without removing anything")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm uninstall operation")
	cmd.Flags().StringVar(&etcDir, "etc-dir", "/etc/veil", "Veil configuration directory to remove")
	cmd.Flags().StringVar(&varDir, "var-dir", "/var/lib/veil", "Veil state directory to remove")
	cmd.Flags().StringVar(&systemdDir, "systemd-dir", defaultSystemdDir, "systemd unit directory to remove Veil units from")
	cmd.Flags().StringVar(&installDir, "install-dir", "/usr/local/bin", "directory containing the veil binary")

	return cmd
}

func uninstallPlan(opts uninstallWorkflowOptions) string {
	return uninstallflow.Plan(toUninstallFlowOptions(opts))
}
