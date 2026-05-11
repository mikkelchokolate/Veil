package installer

import "github.com/veil-panel/veil/internal/renderer"

func PanelSystemdUnits(profile RURecommendedProfile) []string {
	units := []string{renderer.UnitVeil}
	if profile.InstallPanelCaddy {
		units = append(units, renderer.UnitNaive)
	}
	return units
}
