package repair

import (
	"fmt"
	"io"

	"github.com/veil-panel/veil/internal/installer"
)

type Options struct {
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

type Dependencies struct {
	BuildPlan func(Options) (installer.RepairPlan, error)
	ApplyPlan func(installer.RepairPlan, Options) error
}

func Run(opts Options, out io.Writer, deps Dependencies) error {
	if opts.Profile != "ru-recommended" {
		return fmt.Errorf("profile %q is not implemented yet", opts.Profile)
	}
	plan, err := deps.BuildPlan(opts)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "Veil repair plan")
	fmt.Fprintln(out, plan.Summary())
	if opts.DryRun {
		return nil
	}
	return deps.ApplyPlan(plan, opts)
}
