package cli

import (
	"github.com/spf13/cobra"
	repairflow "github.com/veil-panel/veil/internal/cliflow/repair"
	"github.com/veil-panel/veil/internal/installer"
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
	return repairflow.Run(toRepairFlowOptions(opts), cmd.OutOrStdout(), repairflow.Dependencies{
		BuildPlan: func(flowOpts repairflow.Options) (installer.RepairPlan, error) {
			return buildRepairPlanFromOptions(fromRepairFlowOptions(flowOpts))
		},
		ApplyPlan: func(plan installer.RepairPlan, flowOpts repairflow.Options) error {
			return applyRepairPlan(cmd, plan, fromRepairFlowOptions(flowOpts))
		},
	})
}

func toRepairFlowOptions(opts repairWorkflowOptions) repairflow.Options {
	return repairflow.Options{Profile: opts.Profile, DryRun: opts.DryRun, Yes: opts.Yes, EtcDir: opts.EtcDir, VarDir: opts.VarDir, SystemdDir: opts.SystemdDir, BackupDir: opts.BackupDir, BackupDirSet: opts.BackupDirSet, AuditLog: opts.AuditLog}
}

func fromRepairFlowOptions(opts repairflow.Options) repairWorkflowOptions {
	return repairWorkflowOptions{Profile: opts.Profile, DryRun: opts.DryRun, Yes: opts.Yes, EtcDir: opts.EtcDir, VarDir: opts.VarDir, SystemdDir: opts.SystemdDir, BackupDir: opts.BackupDir, BackupDirSet: opts.BackupDirSet, AuditLog: opts.AuditLog}
}
