package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

type repairWorkflowOptions struct {
	Profile      string
	Stack        string
	Domain       string
	Email        string
	SharedPort   int
	DryRun       bool
	Yes          bool
	EtcDir       string
	VarDir       string
	SystemdDir   string
	BackupDir    string
	BackupDirSet bool
	AuditLog     string
}

func runRepairWorkflow(cmd *cobra.Command, opts repairWorkflowOptions) error {
	if opts.Profile != "ru-recommended" {
		return fmt.Errorf("profile %q is not implemented yet", opts.Profile)
	}
	if opts.Domain == "" {
		return fmt.Errorf("--domain is required for ru-recommended profile")
	}
	if opts.Email == "" {
		return fmt.Errorf("--email is required for ru-recommended profile")
	}
	if opts.SharedPort <= 0 || opts.SharedPort > 65535 {
		return fmt.Errorf("--port is required and must be between 1 and 65535")
	}
	plan, err := buildRepairPlanFromOptions(opts)
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Veil repair plan")
	fmt.Fprintln(cmd.OutOrStdout(), plan.Summary())
	if opts.DryRun {
		return nil
	}
	return applyRepairPlan(cmd, plan, opts)
}
