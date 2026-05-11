package install

import (
	"fmt"
	"io"
	"strings"

	"github.com/veil-panel/veil/internal/hostenv"
	"github.com/veil-panel/veil/internal/installer"
)

type Presentation struct {
	out io.Writer
}

func NewPresentation(out io.Writer) Presentation {
	return Presentation{out: out}
}

func (p Presentation) PrintDNSCheck(check hostenv.DNSCheck) {
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

func (p Presentation) PrintRURecommended(profile installer.RURecommendedProfile, dryRun bool) {
	mode := "apply"
	if dryRun {
		mode = "dry run"
	}
	fmt.Fprintf(p.out, "Veil ru-recommended %s\n", mode)
	fmt.Fprintf(p.out, "Domain: %s\n", profile.Domain)
	fmt.Fprintf(p.out, "Email: %s\n", profile.Email)
	fmt.Fprintln(p.out, "Install scope: Panel")
	fmt.Fprintln(p.out, "")
	if profile.InstallPanelCaddy {
		fmt.Fprintln(p.out, "Generated Caddyfile")
		fmt.Fprintln(p.out, strings.Repeat("-", 24))
		fmt.Fprintln(p.out, p.RedactProfileSecrets(profile, profile.Caddyfile))
	}
}

func (Presentation) RedactProfileSecrets(profile installer.RURecommendedProfile, text string) string {
	for _, secret := range []string{profile.PanelAuthToken} {
		if secret == "" {
			continue
		}
		text = strings.ReplaceAll(text, secret, "[REDACTED]")
	}
	return text
}
