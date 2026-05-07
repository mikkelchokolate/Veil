package cli

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"path/filepath"
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
	var parsedPublicIP net.IP
	if opts.PublicIP != "" {
		if opts.PublicIP == "auto" {
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			var detectErr error
			parsedPublicIP, detectErr = installer.DetectPublicIP(ctx, installPublicIPClient, installPublicIPEndpoints)
			if detectErr != nil {
				return detectErr
			}
		} else {
			parsedPublicIP = net.ParseIP(opts.PublicIP)
			if parsedPublicIP == nil {
				return fmt.Errorf("--public-ip must be a valid IPv4 or IPv6 address, or auto")
			}
		}
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
		if opts.Interactive {
			fmt.Fprint(cmd.OutOrStdout(), "Apply install plan? [y/N]: ")
			answer, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
			if err != nil {
				return fmt.Errorf("read confirmation: %w", err)
			}
			if strings.ToLower(strings.TrimSpace(answer)) != "y" {
				return fmt.Errorf("install cancelled")
			}
		} else {
			return fmt.Errorf("apply mode requires --yes; rerun with --dry-run to preview")
		}
	}
	actualBackupDir := opts.BackupDir
	if !opts.BackupDirSet {
		actualBackupDir = filepath.Join(opts.VarDir, "backups")
	}
	result, err := installApplyFunc(built, installer.ApplyPaths{EtcDir: opts.EtcDir, VarDir: opts.VarDir, SystemdDir: opts.SystemdDir, BackupDir: actualBackupDir})
	if err != nil {
		_ = writeAuditInstall(opts.AuditLog, result.BackupID, false, err.Error(), nil)
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Written files:")
	for _, path := range result.WrittenFiles {
		fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", path)
	}
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprint(cmd.OutOrStdout(), installCredentialSummary(built))
	if err := writeAuditInstall(opts.AuditLog, result.BackupID, true, "", result.WrittenFiles); err != nil {
		return fmt.Errorf("audit log write failed after successful install: %w", err)
	}
	return nil
}
