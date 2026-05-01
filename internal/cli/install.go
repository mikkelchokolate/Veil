package cli

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/veil-panel/veil/internal/installer"
)

var installDNSResolver installer.DNSResolver = installer.NetResolver{}
var installPublicIPClient *http.Client
var installPublicIPEndpoints []string

func newInstallCommand() *cobra.Command {
	var profile string
	var stack string
	var domain string
	var email string
	var dryRun bool
	var yes bool
	var etcDir string
	var varDir string
	var systemdDir string
	var panelPort int
	var sharedPort int
	var publicIP string
	var interactive bool
	var hysteriaSHA256 string

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install and configure Veil managed services",
		RunE: func(cmd *cobra.Command, args []string) error {
			if profile != "ru-recommended" {
				return fmt.Errorf("profile %q is not implemented yet", profile)
			}
			if interactive {
				if err := promptInstallOptions(cmd, &domain, &email, &sharedPort, &panelPort); err != nil {
					return err
				}
			}
			if domain == "" {
				return fmt.Errorf("--domain is required for ru-recommended profile")
			}
			if email == "" {
				return fmt.Errorf("--email is required for ru-recommended profile")
			}
			if sharedPort <= 0 || sharedPort > 65535 {
				return fmt.Errorf("--port is required and must be between 1 and 65535")
			}
			var parsedPublicIP net.IP
			if publicIP != "" {
				if publicIP == "auto" {
					ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
					defer cancel()
					var detectErr error
					parsedPublicIP, detectErr = installer.DetectPublicIP(ctx, installPublicIPClient, installPublicIPEndpoints)
					if detectErr != nil {
						return detectErr
					}
				} else {
					parsedPublicIP = net.ParseIP(publicIP)
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
			panelListenPort, panelRandom, err := installer.SelectPanelPort(panelPort, installer.RandomHighPort)
			if err != nil {
				return err
			}
			built, err := installer.BuildRURecommendedProfile(installer.RURecommendedInput{
				Domain:       domain,
				Email:        email,
				Stack:        installer.Stack(stack),
				Port:         sharedPort,
				Availability: availability,
				Secret:       randomSecret,
				RandomPort:   randomPort,
			})
			if err != nil {
				return err
			}
			printRURecommended(cmd, built, dryRun)
			if parsedPublicIP != nil {
				ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
				defer cancel()
				dnsCheck, err := installer.CheckDomainDNS(ctx, installDNSResolver, domain, parsedPublicIP)
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
			plan, planErr := installer.BuildInstallPlan(built, installer.InstallPlanInput{
				Platform:        installer.CurrentPlatform(),
				HysteriaVersion: "v2.6.0",
				HysteriaSHA256:  hysteriaSHA256,
				SystemdUnits:    systemdUnitsForProfile(built),
				PanelPort:       panelListenPort,
			})
			if planErr == nil {
				fmt.Fprintln(cmd.OutOrStdout(), "Install plan")
				fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("-", 12))
				fmt.Fprintln(cmd.OutOrStdout(), plan.Summary())
			}
			if dryRun {
				return nil
			}
			if !yes {
				return fmt.Errorf("apply mode requires --yes; rerun with --dry-run to preview")
			}
			result, err := installer.ApplyRURecommendedProfile(built, installer.ApplyPaths{EtcDir: etcDir, VarDir: varDir, SystemdDir: systemdDir})
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Written files:")
			for _, path := range result.WrittenFiles {
				fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", path)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "default", "install profile: default or ru-recommended")
	cmd.Flags().StringVar(&stack, "stack", "both", "proxy stack to install: both, naive, or hysteria2")
	cmd.Flags().StringVar(&domain, "domain", "", "domain for ACME and client configs")
	cmd.Flags().StringVar(&email, "email", "", "ACME email")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "render installation plan without writing files")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm writing generated files")
	cmd.Flags().StringVar(&etcDir, "etc-dir", "/etc/veil", "Veil configuration directory")
	cmd.Flags().StringVar(&varDir, "var-dir", "/var/lib/veil", "Veil state directory")
	cmd.Flags().StringVar(&systemdDir, "systemd-dir", "", "optional systemd unit output directory, e.g. /etc/systemd/system")
	cmd.Flags().IntVar(&sharedPort, "port", 0, "required shared proxy port for NaiveProxy TCP and/or Hysteria2 UDP")
	cmd.Flags().IntVar(&panelPort, "panel-port", 0, "panel TCP port; 0 selects a random high port")
	cmd.Flags().StringVar(&publicIP, "public-ip", "", "optional server public IP for DNS validation; use auto to detect it")
	cmd.Flags().StringVar(&hysteriaSHA256, "hysteria-sha256", "", "expected sha256 for the Hysteria2 release asset before binary download")
	cmd.Flags().BoolVar(&interactive, "interactive", false, "prompt for missing ru-recommended install options")
	return cmd
}

func systemdUnitsForProfile(profile installer.RURecommendedProfile) []string {
	units := []string{"veil.service"}
	if profile.InstallNaive {
		units = append(units, "veil-naive.service")
	}
	if profile.InstallHysteria2 {
		units = append(units, "veil-hysteria2.service")
	}
	return units
}

func promptInstallOptions(cmd *cobra.Command, domain *string, email *string, sharedPort *int, panelPort *int) error {
	reader := bufio.NewReader(cmd.InOrStdin())
	out := cmd.OutOrStdout()
	if strings.TrimSpace(*domain) == "" {
		fmt.Fprint(out, "Domain for Veil/ACME: ")
		value, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		*domain = strings.TrimSpace(value)
	}
	if strings.TrimSpace(*email) == "" {
		fmt.Fprint(out, "ACME email: ")
		value, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		*email = strings.TrimSpace(value)
	}
	if *sharedPort == 0 {
		fmt.Fprint(out, "Shared proxy port: ")
		value, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("invalid shared proxy port: %w", err)
		}
		*sharedPort = parsed
	}
	if *panelPort == 0 {
		fmt.Fprint(out, "Customize panel port? If no, Veil will choose a random high port. [y/N]: ")
		value, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		answer := strings.ToLower(strings.TrimSpace(value))
		if answer == "y" || answer == "yes" {
			fmt.Fprint(out, "Panel TCP port: ")
			value, err := reader.ReadString('\n')
			if err != nil {
				return err
			}
			parsed, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return fmt.Errorf("invalid panel port: %w", err)
			}
			*panelPort = parsed
		}
	}
	return nil
}

func printDNSCheck(cmd *cobra.Command, check installer.DNSCheck) {
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "DNS check")
	fmt.Fprintln(out, strings.Repeat("-", 9))
	fmt.Fprintf(out, "Domain: %s\n", check.Domain)
	if check.PublicIP != "" {
		fmt.Fprintf(out, "Public IP: %s\n", check.PublicIP)
	}
	if len(check.ResolvedIPs) > 0 {
		fmt.Fprintf(out, "Resolved IPs: %s\n", strings.Join(check.ResolvedIPs, ", "))
	} else {
		fmt.Fprintln(out, "Resolved IPs: none")
	}
	for _, warning := range check.Warnings {
		fmt.Fprintf(out, "Warning: %s\n", warning)
	}
}

func printRURecommended(cmd *cobra.Command, profile installer.RURecommendedProfile, dryRun bool) {
	mode := "apply"
	if dryRun {
		mode = "dry run"
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Veil ru-recommended %s\n", mode)
	fmt.Fprintf(out, "Domain: %s\n", profile.Domain)
	fmt.Fprintf(out, "Email: %s\n", profile.Email)
	fmt.Fprintf(out, "Stack: %s\n", stackName(profile))
	if profile.PortPlan.Changed {
		fmt.Fprintf(out, "Port changed: %s\n", profile.PortPlan.Reason)
	}
	if profile.InstallNaive {
		fmt.Fprintf(out, "NaiveProxy TCP port: %d\n", profile.PortPlan.Naive.Port)
	}
	if profile.InstallHysteria2 {
		fmt.Fprintf(out, "Hysteria2 UDP port: %d\n", profile.PortPlan.Hysteria2.Port)
	}
	if profile.InstallNaive {
		fmt.Fprintf(out, "NaiveProxy client URL: %s\n", redactProfileSecrets(profile, profile.NaiveClientURL))
	}
	if profile.InstallHysteria2 {
		fmt.Fprintf(out, "Hysteria2 client URI: %s\n", redactProfileSecrets(profile, profile.Hysteria2ClientURI))
	}
	fmt.Fprintln(out, "")
	if profile.InstallNaive {
		fmt.Fprintln(out, "Generated Caddyfile")
		fmt.Fprintln(out, strings.Repeat("-", 24))
		fmt.Fprintln(out, redactProfileSecrets(profile, profile.Caddyfile))
	}
	if profile.InstallHysteria2 {
		fmt.Fprintln(out, "Generated Hysteria2 server.yaml")
		fmt.Fprintln(out, strings.Repeat("-", 32))
		fmt.Fprintln(out, redactProfileSecrets(profile, profile.Hysteria2YAML))
	}
}

func redactProfileSecrets(profile installer.RURecommendedProfile, text string) string {
	for _, secret := range []string{profile.NaivePassword, profile.Hysteria2Password, profile.PanelAuthToken} {
		if secret == "" {
			continue
		}
		text = strings.ReplaceAll(text, secret, "[REDACTED]")
	}
	return text
}

func stackName(profile installer.RURecommendedProfile) string {
	switch {
	case profile.InstallNaive && profile.InstallHysteria2:
		return string(installer.StackBoth)
	case profile.InstallNaive:
		return string(installer.StackNaive)
	case profile.InstallHysteria2:
		return string(installer.StackHysteria2)
	default:
		return "none"
	}
}

func randomSecret(label string) string {
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return label + "-change-me"
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}
