package installer

import (
	"fmt"
	"strings"

	"github.com/veil-panel/veil/internal/firewall"
	"github.com/veil-panel/veil/internal/service"
)

type InstallPlanInput struct {
	Platform        Platform
	HysteriaVersion string
	SystemdUnits    []string
	PanelPort       int
}

type InstallPlan struct {
	Profile         RURecommendedProfile
	Platform        Platform
	HysteriaURL     string
	CaddyBuild      BuildHint
	SystemdActions  []service.SystemdAction
	FirewallActions []firewall.Rule
}

func BuildInstallPlan(profile RURecommendedProfile, input InstallPlanInput) (InstallPlan, error) {
	if input.Platform.OS == "" {
		input.Platform = CurrentPlatform()
	}
	if input.HysteriaVersion == "" {
		input.HysteriaVersion = "v2.6.0"
	}
	if err := ValidateLinuxPlatform(input.Platform); err != nil {
		return InstallPlan{}, err
	}
	arch, err := NormalizeArch(input.Platform.Arch)
	if err != nil {
		return InstallPlan{}, err
	}
	hysteriaURL, err := Hysteria2ReleaseAssetURL(input.HysteriaVersion, input.Platform.OS, arch)
	if err != nil {
		return InstallPlan{}, err
	}
	return InstallPlan{
		Profile:         profile,
		Platform:        Platform{OS: input.Platform.OS, Arch: arch},
		HysteriaURL:     hysteriaURL,
		CaddyBuild:      CaddyNaiveBuildHint("/usr/local/bin/caddy"),
		SystemdActions:  service.SystemdApplyPlan(input.SystemdUnits),
		FirewallActions: firewall.UFWPlan(firewall.Config{SharedPort: profile.PortPlan.Port, PanelPort: input.PanelPort}),
	}, nil
}

func (p InstallPlan) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Shared port: %d\n", p.Profile.PortPlan.Port)
	fmt.Fprintf(&b, "NaiveProxy: tcp/%d\n", p.Profile.PortPlan.Naive.Port)
	fmt.Fprintf(&b, "Hysteria2: udp/%d\n", p.Profile.PortPlan.Hysteria2.Port)
	fmt.Fprintf(&b, "Hysteria2 asset: %s\n", p.HysteriaURL)
	fmt.Fprintf(&b, "Caddy/NaiveProxy build: %s\n", p.CaddyBuild.BinaryPath)
	for _, command := range p.CaddyBuild.Commands {
		fmt.Fprintf(&b, "- %s\n", command)
	}
	for _, action := range p.SystemdActions {
		fmt.Fprintf(&b, "%s %s\n", action.Command, strings.Join(action.Args, " "))
	}
	for _, action := range p.FirewallActions {
		fmt.Fprintf(&b, "%s %s\n", action.Command, strings.Join(action.Args, " "))
	}
	return b.String()
}
