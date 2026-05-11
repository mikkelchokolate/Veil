package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	installflow "github.com/veil-panel/veil/internal/cliflow/install"
	"github.com/veil-panel/veil/internal/hostenv"
	"github.com/veil-panel/veil/internal/installer"
	"github.com/veil-panel/veil/internal/panelaccess"
)

const defaultSystemdDir = "/etc/systemd/system"

type ruRecommendedInstallOptions struct {
	Profile      string
	Domain       string
	Email        string
	DryRun       bool
	Yes          bool
	EtcDir       string
	VarDir       string
	SystemdDir   string
	PanelPort    int
	PanelPortSet bool
	PanelAccess  string
	PublicIP     string
	Interactive  bool
	AuditLog     string
	BackupDir    string
	BackupDirSet bool
	CaddyBinary  string
}

type RURecommendedInstallWorkflow struct {
	cmd  *cobra.Command
	opts ruRecommendedInstallOptions
}

func NewRURecommendedInstallWorkflow(cmd *cobra.Command, opts ruRecommendedInstallOptions) RURecommendedInstallWorkflow {
	return RURecommendedInstallWorkflow{cmd: cmd, opts: opts}
}

func runRURecommendedInstall(cmd *cobra.Command, opts ruRecommendedInstallOptions) error {
	return NewRURecommendedInstallWorkflow(cmd, opts).Run()
}

func validateRURecommendedInstallRequirements(opts ruRecommendedInstallOptions) error {
	if opts.PanelAccess == "caddy" && (opts.Domain == "" || opts.Email == "") {
		return fmt.Errorf("--domain and --email are required for caddy Panel access")
	}
	return nil
}

func buildRURecommendedInstallFromOptions(opts ruRecommendedInstallOptions) (installer.RURecommendedInstall, error) {
	return installer.BuildRURecommendedInstall(installer.RURecommendedInstallInput{
		Domain:          opts.Domain,
		Email:           opts.Email,
		PanelAccess:     opts.PanelAccess,
		PanelPort:       opts.PanelPort,
		Secret:          randomSecret,
		RandomPanelPort: installer.RandomHighPort,
	})
}

func (w RURecommendedInstallWorkflow) Run() error {
	cmd := w.cmd
	opts := w.opts
	if opts.Profile != "ru-recommended" {
		return fmt.Errorf("profile %q is not implemented yet", opts.Profile)
	}
	if opts.Interactive {
		if err := promptInstallOptions(cmd, opts.PanelAccess, &opts.Domain, &opts.Email, &opts.PanelPort); err != nil {
			return err
		}
	}
	if err := validateRURecommendedInstallRequirements(opts); err != nil {
		return err
	}
	parsedPublicIP, err := hostenv.ResolvePublicIP(cmd.Context(), opts.PublicIP, installPublicIPClient, installPublicIPEndpoints)
	if err != nil {
		return err
	}

	install, err := buildRURecommendedInstallFromOptions(opts)
	if err != nil {
		return err
	}
	built := install.Profile
	panelListenPort := install.PanelPort
	panelRandom := install.PanelRandom
	printRURecommended(cmd, built, opts.DryRun)
	if parsedPublicIP != nil {
		ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
		defer cancel()
		dnsCheck, err := hostenv.CheckDomainDNS(ctx, installDNSResolver, opts.Domain, parsedPublicIP)
		if err != nil {
			return err
		}
		printDNSCheck(cmd, dnsCheck)
	}
	if _, err := panelaccess.NewMode(opts.PanelAccess).Resolve(panelListenPort); err != nil {
		return err
	}
	fmt.Fprint(cmd.OutOrStdout(), installflow.NewPanelSummary(installflow.PanelSummaryInput{Profile: built, PanelPort: panelListenPort, PanelRandom: panelRandom, PanelPortSet: opts.PanelPortSet}).String())
	planSummary, planErr := buildRURecommendedInstallPlanSummary(built, panelListenPort)
	if planErr == nil {
		fmt.Fprintln(cmd.OutOrStdout(), "Install plan")
		fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("-", 12))
		fmt.Fprintln(cmd.OutOrStdout(), planSummary)
	}
	if opts.DryRun {
		return nil
	}
	caddyBinary, err := validateInstallRuntimePrerequisites(built)
	if err != nil {
		return err
	}
	opts.CaddyBinary = caddyBinary
	if !opts.Yes {
		if err := installflow.ConfirmPlan(cmd.InOrStdin(), cmd.OutOrStdout(), opts.Interactive); err != nil {
			return err
		}
	}
	return applyRURecommendedInstall(cmd, built, opts)
}

func buildRURecommendedInstallPlanSummary(profile installer.RURecommendedProfile, panelPort int) (string, error) {
	plan, err := installer.BuildInstallPlan(profile, installer.InstallPlanInput{
		Platform:     hostenv.CurrentPlatform(),
		SystemdUnits: installer.PanelSystemdUnits(profile),
		PanelPort:    panelPort,
		CaddyBinary:  installPlanCaddyBinary(profile),
	})
	if err != nil {
		return "", err
	}
	return plan.Summary(), nil
}

func installPlanCaddyBinary(profile installer.RURecommendedProfile) string {
	if !profile.InstallPanelCaddy {
		return ""
	}
	path, err := commandLookPath("caddy")
	if err != nil {
		return ""
	}
	return path
}

func validateInstallRuntimePrerequisites(profile installer.RURecommendedProfile) (string, error) {
	if profile.InstallPanelCaddy {
		path, err := commandLookPath("caddy")
		if err != nil {
			return "", fmt.Errorf("caddy is required for caddy Panel access; install Caddy or use --panel-access local/direct")
		}
		return path, nil
	}
	return "", nil
}
