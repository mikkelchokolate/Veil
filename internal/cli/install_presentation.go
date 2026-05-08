package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/veil-panel/veil/internal/installer"
)

type InstallPresentation struct {
	out io.Writer
}

func NewInstallPresentation(out io.Writer) InstallPresentation {
	return InstallPresentation{out: out}
}

func (p InstallPresentation) PrintDNSCheck(check installer.DNSCheck) {
	fmt.Fprintln(p.out, "DNS check")
	fmt.Fprintln(p.out, strings.Repeat("-", 9))
	fmt.Fprintf(p.out, "Domain: %s\n", check.Domain)
	if check.PublicIP != "" {
		fmt.Fprintf(p.out, "Public IP: %s\n", check.PublicIP)
	}
	if len(check.ResolvedIPs) > 0 {
		fmt.Fprintf(p.out, "Resolved IPs: %s\n", strings.Join(check.ResolvedIPs, ", "))
	} else {
		fmt.Fprintln(p.out, "Resolved IPs: none")
	}
	for _, warning := range check.Warnings {
		fmt.Fprintf(p.out, "Warning: %s\n", warning)
	}
}

func (p InstallPresentation) PrintRURecommended(profile installer.RURecommendedProfile, dryRun bool) {
	mode := "apply"
	if dryRun {
		mode = "dry run"
	}
	fmt.Fprintf(p.out, "Veil ru-recommended %s\n", mode)
	fmt.Fprintf(p.out, "Domain: %s\n", profile.Domain)
	fmt.Fprintf(p.out, "Email: %s\n", profile.Email)
	fmt.Fprintf(p.out, "Stack: %s\n", p.StackName(profile))
	if profile.PortPlan.Changed {
		fmt.Fprintf(p.out, "Port changed: %s\n", profile.PortPlan.Reason)
	}
	if profile.InstallNaive {
		fmt.Fprintf(p.out, "NaiveProxy TCP port: %d\n", profile.PortPlan.Naive.Port)
	}
	if profile.InstallHysteria2 {
		fmt.Fprintf(p.out, "Hysteria2 UDP port: %d\n", profile.PortPlan.Hysteria2.Port)
	}
	if profile.InstallNaive {
		fmt.Fprintf(p.out, "NaiveProxy client URL: %s\n", p.RedactProfileSecrets(profile, profile.NaiveClientURL))
	}
	if profile.InstallHysteria2 {
		fmt.Fprintf(p.out, "Hysteria2 client URI: %s\n", p.RedactProfileSecrets(profile, profile.Hysteria2ClientURI))
	}
	fmt.Fprintln(p.out, "")
	if profile.InstallNaive || profile.InstallPanelCaddy {
		fmt.Fprintln(p.out, "Generated Caddyfile")
		fmt.Fprintln(p.out, strings.Repeat("-", 24))
		fmt.Fprintln(p.out, p.RedactProfileSecrets(profile, profile.Caddyfile))
	}
	if profile.InstallHysteria2 {
		fmt.Fprintln(p.out, "Generated Hysteria2 server.yaml")
		fmt.Fprintln(p.out, strings.Repeat("-", 32))
		fmt.Fprintln(p.out, p.RedactProfileSecrets(profile, profile.Hysteria2YAML))
	}
}

func (InstallPresentation) RedactProfileSecrets(profile installer.RURecommendedProfile, text string) string {
	for _, secret := range []string{profile.NaivePassword, profile.Hysteria2Password, profile.PanelAuthToken} {
		if secret == "" {
			continue
		}
		text = strings.ReplaceAll(text, secret, "[REDACTED]")
	}
	return text
}

func (InstallPresentation) StackName(profile installer.RURecommendedProfile) string {
	switch {
	case profile.InstallNaive && profile.InstallHysteria2:
		return string(installer.StackBoth)
	case profile.InstallNaive:
		return string(installer.StackNaive)
	case profile.InstallHysteria2:
		return string(installer.StackHysteria2)
	case profile.InstallMieru:
		return string(installer.StackMieru)
	default:
		return string(installer.StackPanel)
	}
}

func printDNSCheck(cmd *cobra.Command, check installer.DNSCheck) {
	NewInstallPresentation(cmd.OutOrStdout()).PrintDNSCheck(check)
}

func printRURecommended(cmd *cobra.Command, profile installer.RURecommendedProfile, dryRun bool) {
	NewInstallPresentation(cmd.OutOrStdout()).PrintRURecommended(profile, dryRun)
}

func redactProfileSecrets(profile installer.RURecommendedProfile, text string) string {
	return NewInstallPresentation(io.Discard).RedactProfileSecrets(profile, text)
}

func stackName(profile installer.RURecommendedProfile) string {
	return NewInstallPresentation(io.Discard).StackName(profile)
}
