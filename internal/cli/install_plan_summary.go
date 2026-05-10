package cli

import (
	"github.com/veil-panel/veil/internal/hostenv"
	"github.com/veil-panel/veil/internal/installer"
	"github.com/veil-panel/veil/internal/renderer"
)

func buildRURecommendedInstallPlanSummary(profile installer.RURecommendedProfile, panelPort int) (string, error) {
	plan, err := installer.BuildInstallPlan(profile, installer.InstallPlanInput{
		Platform:     hostenv.CurrentPlatform(),
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
	units := []string{renderer.UnitVeil}
	if profile.InstallPanelCaddy {
		units = append(units, renderer.UnitNaive)
	}
	return units
}
