package installer

import (
	"github.com/veil-panel/veil/internal/firewall"
	"github.com/veil-panel/veil/internal/hostenv"
	"github.com/veil-panel/veil/internal/service"
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
	return NewInstallPlanSummary(p).String()
}
