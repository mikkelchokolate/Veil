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
	var hysteriaURL string
	var hysteriaBinary BinaryAcquisition
	if profile.InstallHysteria2 {
		hysteriaURL, err = Hysteria2ReleaseAssetURL(input.HysteriaVersion, input.Platform.OS, arch)
		if err != nil {
			return InstallPlan{}, err
		}
		hysteriaBinary = BinaryAcquisition{Name: "hysteria2", URL: hysteriaURL, Destination: "/usr/local/bin/hysteria", SHA256: strings.TrimSpace(input.HysteriaSHA256)}
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
	var b strings.Builder
	fmt.Fprintf(&b, "Shared port: %d\n", p.Profile.PortPlan.Port)
	if p.Profile.InstallNaive {
		fmt.Fprintf(&b, "NaiveProxy: tcp/%d\n", p.Profile.PortPlan.Naive.Port)
	}
	if p.Profile.InstallHysteria2 {
		fmt.Fprintf(&b, "Hysteria2: udp/%d\n", p.Profile.PortPlan.Hysteria2.Port)
		fmt.Fprintf(&b, "Hysteria2 asset: %s\n", p.HysteriaURL)
		fmt.Fprintf(&b, "Hysteria2 install path: %s\n", p.HysteriaBinary.Destination)
		if p.HysteriaBinary.SHA256 == "" {
			fmt.Fprintf(&b, "Hysteria2 sha256: required before binary download\n")
		} else {
			fmt.Fprintf(&b, "Hysteria2 sha256: %s\n", p.HysteriaBinary.SHA256)
		}
	}
	if p.Profile.InstallNaive {
		fmt.Fprintf(&b, "Caddy/NaiveProxy build: %s\n", p.CaddyBuild.BinaryPath)
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
