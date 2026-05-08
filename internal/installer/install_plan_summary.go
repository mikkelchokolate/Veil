package installer

import (
	"fmt"
	"strings"
)

type InstallPlanSummary struct {
	plan InstallPlan
}

func NewInstallPlanSummary(plan InstallPlan) InstallPlanSummary {
	return InstallPlanSummary{plan: plan}
}

func (s InstallPlanSummary) String() string {
	p := s.plan
	var b strings.Builder
	if p.Profile.InstallHysteria2 {
		fmt.Fprintf(&b, "Hysteria2 asset: %s\n", p.HysteriaURL)
		fmt.Fprintf(&b, "Hysteria2 install path: %s\n", p.HysteriaBinary.Destination)
		if p.HysteriaBinary.SHA256 == "" {
			fmt.Fprintf(&b, "Hysteria2 sha256: required before binary download\n")
		} else {
			fmt.Fprintf(&b, "Hysteria2 sha256: %s\n", p.HysteriaBinary.SHA256)
		}
	}
	if p.Profile.InstallMieru {
		fmt.Fprintf(&b, "Mieru asset: %s\n", p.MieruBinary.URL)
		fmt.Fprintf(&b, "Mieru install path: %s\n", p.MieruBinary.Destination)
		if p.MieruBinary.SHA256 == "" {
			fmt.Fprintf(&b, "Mieru sha256: required before binary download\n")
		} else {
			fmt.Fprintf(&b, "Mieru sha256: %s\n", p.MieruBinary.SHA256)
		}
	}
	if p.Profile.InstallNaive || p.Profile.InstallPanelCaddy {
		label := "Caddy/NaiveProxy build"
		if p.Profile.InstallPanelCaddy && !p.Profile.InstallNaive {
			label = "Caddy/Panel reverse proxy"
		}
		fmt.Fprintf(&b, "%s: %s\n", label, p.CaddyBuild.BinaryPath)
		for _, command := range p.CaddyBuild.Commands {
			fmt.Fprintf(&b, "- %s\n", command)
		}
	}
	for _, tool := range p.PanelTools {
		fmt.Fprintf(&b, "Panel speedtest tool: %s\n", tool)
	}
	for _, action := range p.SystemdActions {
		fmt.Fprintf(&b, "%s %s\n", action.Command, strings.Join(action.Args, " "))
	}
	for _, action := range p.FirewallActions {
		fmt.Fprintf(&b, "%s %s\n", action.Command, strings.Join(action.Args, " "))
	}
	return b.String()
}
