package installer

import (
	"fmt"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/firewall"
	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/mikkelchokolate/Veil/internal/renderer"
	"github.com/mikkelchokolate/Veil/internal/service"
)

type BuildHint struct {
	BinaryPath string
	Commands   []string
}

func CaddyPanelBuildHint(binaryPath string) BuildHint {
	if binaryPath == "" {
		binaryPath = "/usr/local/bin/caddy"
	}
	return BuildHint{BinaryPath: binaryPath, Commands: []string{"requires standard Caddy at " + binaryPath}}
}

func PanelSystemdUnits(profile RURecommendedProfile) []string {
	units := []string{renderer.UnitHelperSocket, renderer.UnitVeil}
	if profile.InstallPanelCaddy {
		units = append(units, "veil-caddy@panel.service")
	}
	return units
}

type InstallPlanInput struct {
	Platform     hostenv.Platform
	SystemdUnits []string
	PanelPort    int
	CaddyBinary  string
}

type InstallPlan struct {
	Profile         RURecommendedProfile
	Platform        hostenv.Platform
	CaddyBuild      BuildHint
	SystemdActions  []service.SystemdAction
	FirewallActions []firewall.Rule
	PanelTools      []string
}

func BuildInstallPlan(profile RURecommendedProfile, input InstallPlanInput) (InstallPlan, error) {
	if input.Platform.OS == "" {
		input.Platform = hostenv.CurrentPlatform()
	}
	if err := hostenv.ValidateLinuxPlatform(input.Platform); err != nil {
		return InstallPlan{}, err
	}
	arch, err := hostenv.NormalizeArch(input.Platform.Arch)
	if err != nil {
		return InstallPlan{}, err
	}
	caddyBinary := input.CaddyBinary
	if caddyBinary == "" {
		caddyBinary = "/usr/local/bin/caddy"
	}
	var caddyBuild BuildHint
	if profile.InstallPanelCaddy {
		caddyBuild = CaddyPanelBuildHint(caddyBinary)
	}
	panelPort := input.PanelPort
	panelHTTPSPort := 0
	if profile.InstallPanelCaddy {
		panelPort = 0
		panelHTTPSPort = 443
	}
	return InstallPlan{
		Profile:        profile,
		Platform:       hostenv.Platform{OS: input.Platform.OS, Arch: arch},
		CaddyBuild:     caddyBuild,
		SystemdActions: service.SystemdApplyPlan(input.SystemdUnits),
		FirewallActions: firewall.UFWPlan(firewall.Config{
			PanelPort:      panelPort,
			PanelHTTPSPort: panelHTTPSPort,
		}),
		PanelTools: []string{"speedtest-cli or speedtest"},
	}, nil
}

func (p InstallPlan) Summary() string {
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
