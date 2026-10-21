package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	uninstallflow "github.com/mikkelchokolate/Veil/internal/cliflow/uninstall"
	"github.com/spf13/cobra"
)

var uninstallServiceStopper = uninstallflow.StopAndDisableService
var uninstallFileRemover = uninstallflow.RemovePath
var uninstallSystemdReloader = uninstallflow.ReloadSystemdDaemon
var uninstallFirewallCleaner = uninstallflow.RemoveManagedFirewallRules

func newUninstallCommand() *cobra.Command {
	var dryRun bool
	var yes bool
	var purge bool
	var keepData bool
	var etcDir string
	var varDir string
	var systemdDir string
	var installDir string
	caddyStateDir := strings.TrimSpace(os.Getenv("VEIL_CADDY_STATE_DIR"))
	if caddyStateDir == "" {
		caddyStateDir = "/var/lib/caddy"
	}
	mitaStateDir := strings.TrimSpace(os.Getenv("VEIL_MITA_STATE_DIR"))
	if mitaStateDir == "" {
		mitaStateDir = "/var/lib/mita"
	}
	// Packaged vendor unit dirs and the QUIC sysctl drop-in are cleaned by
	// default (issues #492, #486); the env overrides exist for the same
	// test/alternate-layout flexibility as scripts/uninstall.sh.
	vendorSystemdDirs := strings.Fields(os.Getenv("VEIL_VENDOR_SYSTEMD_DIRS"))
	sysctlConfPath := strings.TrimSpace(os.Getenv("VEIL_SYSCTL_CONF"))

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove Veil panel, services, configuration, and state",
		Long: "Remove the Veil panel, services, configuration, and state.\n\n" +
			"By default this also removes the encrypted state and credentials in /etc/veil and\n" +
			"/var/lib/veil, so a later `veil install` starts fresh with a new password and panel\n" +
			"path. Pass --keep-data to preserve configuration and credentials across reinstalls.\n" +
			"Purge also removes the Caddy/Mita state dirs (--caddy-state-dir, --mita-state-dir,\n" +
			"defaulting to VEIL_CADDY_STATE_DIR / VEIL_MITA_STATE_DIR or /var/lib/caddy and\n" +
			"/var/lib/mita); use --keep-data if they are shared with a system Caddy/mita.\n" +
			"A full removal also deletes Veil-managed UFW allow rules (comment \"Veil …\"),\n" +
			"keeping \"Veil management SSH\"; --keep-data leaves the firewall untouched.\n" +
			"A full removal also deletes the runtime binaries Veil installed into --install-dir\n" +
			"(only those whose bytes match Veil's install record); --keep-data keeps them.\n" +
			"The veil system account is always preserved.",
		RunE: func(cmd *cobra.Command, args []string) error {
			warnUninstallEnvOverrides(cmd.ErrOrStderr())
			return uninstallflow.Run(uninstallflow.Options{DryRun: dryRun, Yes: yes, Purge: purge, KeepData: keepData, EtcDir: etcDir, VarDir: varDir, SystemdDir: systemdDir, InstallDir: installDir, CaddyStateDir: caddyStateDir, MitaStateDir: mitaStateDir, VendorSystemdDirs: vendorSystemdDirs, SysctlConfPath: sysctlConfPath}, cmd.OutOrStdout(), cmd.ErrOrStderr(), uninstallflow.Dependencies{
				ServiceStopper:  uninstallServiceStopper,
				FileRemover:     uninstallFileRemover,
				SystemdReloader: uninstallSystemdReloader,
				FirewallCleaner: uninstallFirewallCleaner,
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
	cmd.Flags().StringVar(&caddyStateDir, "caddy-state-dir", caddyStateDir, "Caddy state directory removed on purge (env VEIL_CADDY_STATE_DIR)")
	cmd.Flags().StringVar(&mitaStateDir, "mita-state-dir", mitaStateDir, "Mita state directory removed on purge (env VEIL_MITA_STATE_DIR)")

	return cmd
}

// uninstallEnvOverrides are the VEIL_* variables that redirect uninstall's
// destructive targets. They exist for tests and alternate layouts, but they
// are read from the (root) process environment — a `sudo -E` or an exported
// shell profile silently retargets removals (issue #1263) — so any override
// is surfaced on stderr before the plan runs.
var uninstallEnvOverrides = []struct{ name, packaged string }{
	{"VEIL_CADDY_STATE_DIR", "/var/lib/caddy"},
	{"VEIL_MITA_STATE_DIR", "/var/lib/mita"},
	{"VEIL_VENDOR_SYSTEMD_DIRS", "/lib/systemd/system /usr/lib/systemd/system"},
	{"VEIL_SYSCTL_CONF", "/etc/sysctl.d/" + uninstallflow.QUICSysctlConfName},
}

func warnUninstallEnvOverrides(errOut io.Writer) {
	for _, override := range uninstallEnvOverrides {
		value := strings.Join(strings.Fields(os.Getenv(override.name)), " ")
		if value == "" || value == override.packaged {
			continue
		}
		fmt.Fprintf(errOut, "warning: %s=%s from the environment overrides the packaged uninstall target %s\n", override.name, value, override.packaged)
	}
}
