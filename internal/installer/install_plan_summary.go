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
	if p.Profile.InstallPanelCaddy {
		fmt.Fprintf(&b, "Caddy/Panel reverse proxy: %s\n", p.CaddyBuild.BinaryPath)
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
