package cli

import "github.com/veil-panel/veil/internal/installer"

func buildRURecommendedInstallPlanSummary(profile installer.RURecommendedProfile, panelPort int, hysteriaSHA256 string) (string, error) {
	plan, err := installer.BuildInstallPlan(profile, installer.InstallPlanInput{
		Platform:        installer.CurrentPlatform(),
		HysteriaVersion: "v2.6.0",
		HysteriaSHA256:  hysteriaSHA256,
		SystemdUnits:    systemdUnitsForProfile(profile),
		PanelPort:       panelPort,
		CaddyBinary:     installPlanCaddyBinary(profile),
	})
	if err != nil {
		return "", err
	}
	return plan.Summary(), nil
}

func installPlanCaddyBinary(profile installer.RURecommendedProfile) string {
	if !profile.InstallPanelCaddy && !profile.InstallNaive {
		return ""
	}
	path, err := commandLookPath("caddy")
	if err != nil {
		return ""
	}
	return path
}

func systemdUnitsForProfile(profile installer.RURecommendedProfile) []string {
	units := []string{"veil.service"}
	if profile.InstallNaive || profile.InstallPanelCaddy {
		units = append(units, "veil-naive.service")
	}
	if profile.InstallHysteria2 {
		units = append(units, "veil-hysteria2.service")
	}
	if profile.InstallMieru {
		units = append(units, "veil-mieru.service")
	}
	return units
}
