package cli

import "github.com/veil-panel/veil/internal/installer"

func buildRURecommendedInstallPlanSummary(profile installer.RURecommendedProfile, panelPort int) (string, error) {
	plan, err := installer.BuildInstallPlan(profile, installer.InstallPlanInput{
		Platform:     installer.CurrentPlatform(),
		SystemdUnits: systemdUnitsForProfile(profile),
		PanelPort:    panelPort,
		CaddyBinary:  installPlanCaddyBinary(profile),
	})
	if err != nil {
		return "", err
	}
	return plan.Summary(), nil
}

func installPlanCaddyBinary(profile installer.RURecommendedProfile) string {
	if !profile.InstallPanelCaddy {
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
	if profile.InstallPanelCaddy {
		units = append(units, "veil-naive.service")
	}
	return units
}
