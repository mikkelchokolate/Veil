package installer

import (
	"github.com/veil-panel/veil/internal/firewall"
	"github.com/veil-panel/veil/internal/service"
)

type InstallPlanInput struct {
	Platform        Platform
	HysteriaVersion string
	HysteriaSHA256  string
	SystemdUnits    []string
	PanelPort       int
}

type InstallPlan struct {
	Profile         RURecommendedProfile
	Platform        Platform
	HysteriaURL     string
	HysteriaBinary  BinaryAcquisition
	CaddyBuild      BuildHint
	SystemdActions  []service.SystemdAction
	FirewallActions []firewall.Rule
	PanelTools      []string
}

type BinaryAcquisition struct {
	Name        string
	URL         string
	Destination string
	SHA256      string
}

func BuildInstallPlan(profile RURecommendedProfile, input InstallPlanInput) (InstallPlan, error) {
	input = NewInstallPlanDefaults(nil).Apply(input)
	if err := ValidateLinuxPlatform(input.Platform); err != nil {
		return InstallPlan{}, err
	}
	arch, err := NormalizeArch(input.Platform.Arch)
	if err != nil {
		return InstallPlan{}, err
	}
	var hysteriaURL string
	var hysteriaBinary BinaryAcquisition
	if profile.InstallHysteria2 {
		artifact, err := NewHysteriaBinaryAcquisition().Build(input.HysteriaVersion, input.Platform.OS, arch, input.HysteriaSHA256)
		if err != nil {
			return InstallPlan{}, err
		}
		hysteriaURL = artifact.URL
		hysteriaBinary = artifact.Binary
	}
	var caddyBuild BuildHint
	if profile.InstallNaive {
		caddyBuild = CaddyNaiveBuildHint("/usr/local/bin/caddy")
	}
	return InstallPlan{
		Profile:        profile,
		Platform:       Platform{OS: input.Platform.OS, Arch: arch},
		HysteriaURL:    hysteriaURL,
		HysteriaBinary: hysteriaBinary,
		CaddyBuild:     caddyBuild,
		SystemdActions: service.SystemdApplyPlan(input.SystemdUnits),
		FirewallActions: firewall.UFWPlan(firewall.Config{
			SharedPort: profile.PortPlan.Port,
			PanelPort:  input.PanelPort,
			EnableTCP:  profile.InstallNaive,
			EnableUDP:  profile.InstallHysteria2,
		}),
		PanelTools: []string{"speedtest-cli or speedtest"},
	}, nil
}

func (p InstallPlan) Summary() string {
	return NewInstallPlanSummary(p).String()
}
