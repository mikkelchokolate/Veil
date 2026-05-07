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
			return NewUninstallWorkflow(uninstallWorkflowOptions{DryRun: dryRun, Yes: yes}, cmd.OutOrStdout(), cmd.ErrOrStderr()).Run()
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print uninstall plan without removing anything")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm uninstall operation")

	return cmd
}

func uninstallPlan() string {
	var b strings.Builder
	b.WriteString("Stop services:\n")
	for _, svc := range uninstallServices() {
		b.WriteString(fmt.Sprintf("  - %s\n", svc))
	}
	b.WriteString("Disable services:\n")
	for _, svc := range uninstallServices() {
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
