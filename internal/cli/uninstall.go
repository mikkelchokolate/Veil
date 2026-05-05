package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

var uninstallServiceStopper = stopAndDisableService
var uninstallFileRemover = removePath

func newUninstallCommand() *cobra.Command {
	var dryRun bool
	var yes bool

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove Veil panel, services, and configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			plan := uninstallPlan()

			fmt.Fprintln(cmd.OutOrStdout(), "Veil uninstall plan")
			fmt.Fprintln(cmd.OutOrStdout(), plan)

			if dryRun {
				return nil
			}

			if !yes {
				return fmt.Errorf("uninstall requires --yes; rerun with --dry-run to preview")
			}

			// Stop and disable services
			for _, svc := range []string{"veil.service", "veil-naive.service", "veil-hysteria2.service", "veil-warp.service"} {
				if err := uninstallServiceStopper(svc); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: service %s: %v\n", svc, err)
				}
			}

			// Remove files and directories
			for _, path := range []string{"/etc/veil", "/var/lib/veil", "/usr/local/bin/veil"} {
				if err := uninstallFileRemover(path); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: remove %s: %v\n", path, err)
				}
			}

			fmt.Fprintln(cmd.OutOrStdout(), "Uninstalled Veil")
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print uninstall plan without removing anything")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm uninstall operation")

	return cmd
}

func uninstallPlan() string {
	var b strings.Builder
	b.WriteString("Stop services:\n")
	for _, svc := range []string{"veil.service", "veil-naive.service", "veil-hysteria2.service", "veil-warp.service"} {
		b.WriteString(fmt.Sprintf("  - %s\n", svc))
	}
	b.WriteString("Disable services:\n")
	for _, svc := range []string{"veil.service", "veil-naive.service", "veil-hysteria2.service", "veil-warp.service"} {
		b.WriteString(fmt.Sprintf("  - %s\n", svc))
	}
	b.WriteString("Remove files:\n")
	for _, path := range []string{"/etc/veil", "/var/lib/veil"} {
		b.WriteString(fmt.Sprintf("  - %s\n", path))
	}
	b.WriteString("Remove binary:\n")
	b.WriteString(fmt.Sprintf("  - %s\n", "/usr/local/bin/veil"))
	return b.String()
}

func stopAndDisableService(service string) error {
	if err := exec.Command("systemctl", "stop", service).Run(); err != nil {
		return fmt.Errorf("stop: %w", err)
	}
	if err := exec.Command("systemctl", "disable", service).Run(); err != nil {
		return fmt.Errorf("disable: %w", err)
	}
	return nil
}

func removePath(path string) error {
	return os.RemoveAll(path)
}
