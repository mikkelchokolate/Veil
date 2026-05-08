package installer

import (
	"github.com/veil-panel/veil/internal/firewall"
	"github.com/veil-panel/veil/internal/service"
)

type InstallPlanInput struct {
	Platform        Platform
	HysteriaVersion string
	HysteriaSHA256  string
	MieruVersion    string
	MieruSHA256     string
	SystemdUnits    []string
	PanelPort       int
}

type InstallPlan struct {
	Profile         RURecommendedProfile
	Platform        Platform
	HysteriaURL     string
	HysteriaBinary  BinaryAcquisition
	MieruBinary     BinaryAcquisition
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
	var mieruBinary BinaryAcquisition
	if profile.InstallMieru {
		artifact, err := NewMieruBinaryAcquisition().Build(input.MieruVersion, input.Platform.OS, arch, input.MieruSHA256)
		if err != nil {
			return InstallPlan{}, err
		}
		mieruBinary = artifact.Binary
	}
	var caddyBuild BuildHint
	if profile.InstallNaive {
		caddyBuild = CaddyNaiveBuildHint("/usr/local/bin/caddy")
	} else if profile.InstallPanelCaddy {
		caddyBuild = CaddyPanelBuildHint("/usr/local/bin/caddy")
	}
	panelPort := input.PanelPort
	panelHTTPSPort := 0
	if profile.InstallPanelCaddy {
		panelPort = 0
		panelHTTPSPort = 443
	}
	return InstallPlan{
		Profile:        profile,
		Platform:       Platform{OS: input.Platform.OS, Arch: arch},
		HysteriaURL:    hysteriaURL,
		HysteriaBinary: hysteriaBinary,
		MieruBinary:    mieruBinary,
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
