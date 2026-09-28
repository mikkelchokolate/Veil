package repair

import (
	"fmt"
	"io"

	"github.com/mikkelchokolate/Veil/internal/installer"
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
	LEIPCert     bool
	LEIPCertPort int
	PublicIP     string
}

// mutates reports whether this run is authorized to change the host. Plan
// building must stay read-only until the operator passes --yes: ACME
// issuance installs packages, pipes a remote script as root, binds :80 and
// rewrites cert material, and the state commit rewrites state.json — none of
// that may run for a bare `veil repair` that ApplyPlan will refuse anyway
// (issue #1128). --dry-run stays read-only even combined with --yes.
func (o Options) mutates() bool { return o.Yes && !o.DryRun }

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
