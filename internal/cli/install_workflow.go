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
	if opts.Domain == "" {
		return fmt.Errorf("--domain is required for ru-recommended profile")
	}
	if opts.Email == "" {
		return fmt.Errorf("--email is required for ru-recommended profile")
	}
	if opts.SharedPort <= 0 || opts.SharedPort > 65535 {
		return fmt.Errorf("--port is required and must be between 1 and 65535")
	}
	parsedPublicIP, err := resolveInstallPublicIP(cmd.Context(), opts.PublicIP)
	if err != nil {
		return err
	}

	availability, err := installer.DetectPortAvailability([]int{443, 8443})
	if err != nil {
		return err
	}
	randomPort := func() int {
		port, err := installer.RandomHighPort()
		if err != nil {
			return 31874
		}
		return port
	}
	install, err := installer.BuildRURecommendedInstall(installer.RURecommendedInstallInput{
		Domain:          opts.Domain,
		Email:           opts.Email,
		Stack:           installer.Stack(opts.Stack),
		Port:            opts.SharedPort,
		PanelPort:       opts.PanelPort,
		Availability:    availability,
		Secret:          randomSecret,
		RandomPort:      randomPort,
		RandomPanelPort: installer.RandomHighPort,
	})
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
	if built.WebBasePath != "" && built.WebBasePath != "/" {
		fmt.Fprintf(cmd.OutOrStdout(), "Panel URL: https://%s%s\n", built.Domain, built.WebBasePath)
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
