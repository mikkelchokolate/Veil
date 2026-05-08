package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

type repairWorkflowOptions struct {
	Profile      string
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
