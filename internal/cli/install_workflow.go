package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/veil-panel/veil/internal/installer"
)

type ruRecommendedInstallOptions struct {
	Profile        string
	Stack          string
	Domain         string
	Email          string
	DryRun         bool
	Yes            bool
	EtcDir         string
	VarDir         string
	SystemdDir     string
	PanelPort      int
	PanelAccess    string
	SharedPort     int
	PublicIP       string
	Interactive    bool
	HysteriaSHA256 string
	AuditLog       string
	BackupDir      string
	BackupDirSet   bool
}

func runRURecommendedInstall(cmd *cobra.Command, opts ruRecommendedInstallOptions) error {
	if opts.Profile != "ru-recommended" {
		return fmt.Errorf("profile %q is not implemented yet", opts.Profile)
	}
	if opts.Interactive {
		if err := promptInstallOptions(cmd, &opts.Domain, &opts.Email, &opts.SharedPort, &opts.PanelPort); err != nil {
			return err
		}
	}
	if strings.TrimSpace(opts.Stack) == "" {
		opts.Stack = "panel"
		if opts.Domain != "" || opts.Email != "" || opts.SharedPort != 0 {
			opts.Stack = "both"
		}
	}
	requiresDomainStack := strings.TrimSpace(opts.Stack) == "both" || strings.TrimSpace(opts.Stack) == "naive" || strings.TrimSpace(opts.Stack) == "hysteria2"
	if requiresDomainStack {
		if opts.Domain == "" {
			return fmt.Errorf("--domain is required for ru-recommended profile")
		}
		if opts.Email == "" {
			return fmt.Errorf("--email is required for ru-recommended profile")
		}
		if opts.SharedPort <= 0 || opts.SharedPort > 65535 {
			return fmt.Errorf("--port is required and must be between 1 and 65535")
		}
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
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Panel port: %d (user selected)\n", panelListenPort)
	}
	panelAccess, err := NewPanelAccessMode(opts.PanelAccess).Resolve(panelListenPort)
	if err != nil {
		return err
	}
	if built.WebBasePath != "" && built.WebBasePath != "/" {
		fmt.Fprintf(cmd.OutOrStdout(), "Panel URL: https://%s%s\n", built.Domain, built.WebBasePath)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Panel access: http://%s/\n", panelAccess.PanelListen)
	}
	planSummary, planErr := buildRURecommendedInstallPlanSummary(built, panelListenPort, opts.HysteriaSHA256)
	if planErr == nil {
		fmt.Fprintln(cmd.OutOrStdout(), "Install plan")
		fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("-", 12))
		fmt.Fprintln(cmd.OutOrStdout(), planSummary)
	}
	if opts.DryRun {
		return nil
	}
	if !opts.Yes {
		if err := confirmInstallPlan(cmd, opts.Interactive); err != nil {
			return err
		}
	}
	return applyRURecommendedInstall(cmd, built, opts)
}
