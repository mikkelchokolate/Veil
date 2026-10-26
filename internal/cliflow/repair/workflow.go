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
	// LEIPCertSet reports whether --le-ip-cert was passed explicitly; only an
	// explicit choice is persisted over an existing VEIL_PANEL_LE_IP_CERT —
	// the true flag default must never overwrite a recorded opt-out (#1187).
	LEIPCertSet     bool
	LEIPCertPort    int
	LEIPCertPortSet bool
	PublicIP        string
}

// mutates reports whether this run is authorized to change the host. Plan
// building must stay read-only until the operator passes --yes: ACME
// issuance installs packages, pipes a remote script as root, binds :80 and
// rewrites cert material, and the state commit rewrites state.json — none of
// that may run for a bare `veil repair` that ApplyPlan will refuse anyway
// (issue #1128). --dry-run stays read-only even combined with --yes.
func (o Options) mutates() bool { return o.Yes && !o.DryRun }

// LEIPCertRequested reports whether this invocation asked for the Let's Encrypt
// IP certificate itself (--le-ip-cert=true or --le-ip-cert-port), as opposed to
// only keeping the persisted preference. A failure then means the operator did
// not get what they asked for, so repair must not report success (#1500).
func (o Options) LEIPCertRequested() bool {
	return (o.LEIPCertSet && o.LEIPCert) || o.LEIPCertPortSet
}

type Dependencies struct {
	BuildPlan func(Options) (installer.RepairPlan, error)
	ApplyPlan func(installer.RepairPlan, Options) error
	// EgressDNSWarning, when set, returns the protocol-unit DNS warning
	// (#1288) or "". Repair re-renders the Hysteria2 and WARP configs, whose
	// resolver choice follows this probe, so the operator sees it next to the
	// plan, including on --dry-run.
	EgressDNSWarning func() string
	// Warnings, when set, returns non-fatal problems noticed while building the
	// plan — a certificate renewal that failed while the old certificate stays
	// in place, a Caddy configuration that could not be re-rendered. They are
	// printed next to the plan so a successful exit never hides them (#1500).
	Warnings func() []string
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
	if deps.Warnings != nil {
		for _, warning := range deps.Warnings() {
			if warning != "" {
				fmt.Fprintln(out, "warning: "+warning)
			}
		}
	}
	if deps.EgressDNSWarning != nil {
		if warning := deps.EgressDNSWarning(); warning != "" {
			fmt.Fprintln(out, "warning: "+warning)
		}
	}
	if opts.DryRun {
		return nil
	}
	return deps.ApplyPlan(plan, opts)
}
