package cli

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/veil-panel/veil/internal/installer"
)

var installDNSResolver installer.DNSResolver = installer.NetResolver{}
var installPublicIPClient *http.Client
var installPublicIPEndpoints []string
var installApplyFunc = installer.ApplyRURecommendedProfile

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
	var auditLog string
	var backupDir string

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install and configure Veil managed services",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRURecommendedInstall(cmd, ruRecommendedInstallOptions{
				Profile:        profile,
				Stack:          stack,
				Domain:         domain,
				Email:          email,
				DryRun:         dryRun,
				Yes:            yes,
				EtcDir:         etcDir,
				VarDir:         varDir,
				SystemdDir:     systemdDir,
				PanelPort:      panelPort,
				SharedPort:     sharedPort,
				PublicIP:       publicIP,
				Interactive:    interactive,
				HysteriaSHA256: hysteriaSHA256,
				AuditLog:       auditLog,
				BackupDir:      backupDir,
				BackupDirSet:   cmd.Flags().Changed("backup-dir"),
			})
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
	cmd.Flags().StringVar(&auditLog, "audit-log", "", "optional path for JSONL audit log")
	cmd.Flags().StringVar(&backupDir, "backup-dir", "", "backup directory for files before overwrite (optional; defaults to var-dir/backups; pass empty string to disable)")
	return cmd
}

func writeAuditInstall(auditLog, backupID string, success bool, errMsg string, writtenFiles []string) error {
	return installer.AppendAuditEvent(auditLog, installer.AuditEvent{
		Action:       "install.apply",
		BackupID:     backupID,
		Success:      success,
		Error:        errMsg,
		WrittenFiles: writtenFiles,
	})
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
	domainPattern := regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)+$`)
	if strings.TrimSpace(*domain) == "" {
		for {
			fmt.Fprint(out, "Domain for Veil/ACME: ")
			value, err := reader.ReadString('\n')
			if err != nil {
				return err
			}
			candidate := strings.TrimSpace(value)
			if candidate == "" {
				fmt.Fprintln(out, "Domain must not be empty.")
				continue
			}
			if !domainPattern.MatchString(candidate) {
				fmt.Fprintln(out, "Domain must be a valid domain name (e.g. example.com).")
				continue
			}
			*domain = candidate
			break
		}
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
		for {
			fmt.Fprint(out, "Shared proxy port: ")
			value, err := reader.ReadString('\n')
			if err != nil {
				return err
			}
			parsed, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				fmt.Fprintln(out, "Port must be a number between 1 and 65535.")
				continue
			}
			if parsed < 1 || parsed > 65535 {
				fmt.Fprintln(out, "Port must be between 1 and 65535.")
				continue
			}
			*sharedPort = parsed
			break
		}
	}
	if *panelPort == 0 {
		fmt.Fprint(out, "Customize panel port? If no, Veil will choose a random high port. [y/N]: ")
		value, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		answer := strings.ToLower(strings.TrimSpace(value))
		if answer == "y" || answer == "yes" {
			for {
				fmt.Fprint(out, "Panel TCP port: ")
				value, err := reader.ReadString('\n')
				if err != nil {
					return err
				}
				parsed, err := strconv.Atoi(strings.TrimSpace(value))
				if err != nil {
					fmt.Fprintln(out, "Port must be a number between 1 and 65535.")
					continue
				}
				if parsed < 1 || parsed > 65535 {
					fmt.Fprintln(out, "Port must be between 1 and 65535.")
					continue
				}
				*panelPort = parsed
				break
			}
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
