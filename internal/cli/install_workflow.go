package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/veil-panel/veil/internal/installer"
)

const defaultSystemdDir = "/etc/systemd/system"

type ruRecommendedInstallOptions struct {
	Profile      string
	Stack        string
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

func runRURecommendedInstall(cmd *cobra.Command, opts ruRecommendedInstallOptions) error {
	if opts.Profile != "ru-recommended" {
		return fmt.Errorf("profile %q is not implemented yet", opts.Profile)
	}
	if opts.Interactive {
		if err := promptInstallOptions(cmd, opts.PanelAccess, &opts.Domain, &opts.Email, &opts.PanelPort); err != nil {
			return err
		}
	}
	opts.Stack = strings.TrimSpace(opts.Stack)
	if opts.Stack == "" {
		opts.Stack = "panel"
	}
	if opts.Stack != "panel" {
		return fmt.Errorf("Veil install only installs Panel; configure protocols as Panel Inbounds")
	}
	if err := NewRURecommendedInstallRequirements().Validate(opts); err != nil {
		return err
	}
	parsedPublicIP, err := resolveInstallPublicIP(cmd.Context(), opts.PublicIP)
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
		dnsCheck, err := installer.CheckDomainDNS(ctx, installDNSResolver, opts.Domain, parsedPublicIP)
		if err != nil {
			return err
		}
		printDNSCheck(cmd, dnsCheck)
	}
	if panelRandom {
		fmt.Fprintf(cmd.OutOrStdout(), "Panel port: %d (random)\n", panelListenPort)
	} else if opts.PanelPortSet {
		fmt.Fprintf(cmd.OutOrStdout(), "Panel port: %d (user selected)\n", panelListenPort)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Panel port: %d (default)\n", panelListenPort)
	}
	panelAccess, err := NewPanelAccessMode(opts.PanelAccess).Resolve(panelListenPort)
	if err != nil {
		return err
	}
	if built.WebBasePath != "" && built.WebBasePath != "/" {
		fmt.Fprintf(cmd.OutOrStdout(), "Panel URL: https://%s%s\n", built.Domain, built.WebBasePath)
	} else {
		scheme := "http"
		if built.PanelTLSEnabled {
			scheme = "https"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Panel access: %s://%s/\n", scheme, panelAccess.PanelListen)
	}
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
		if err := confirmInstallPlan(cmd, opts.Interactive); err != nil {
			return err
		}
	}
	return applyRURecommendedInstall(cmd, built, opts)
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
