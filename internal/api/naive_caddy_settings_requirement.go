package api

import "github.com/veil-panel/veil/internal/panelaccess"

type NaiveCaddySettingsRequirement = panelaccess.NaiveCaddySettingsRequirement

func NewNaiveCaddySettingsRequirement() NaiveCaddySettingsRequirement {
	return panelaccess.NewNaiveCaddySettingsRequirement()
}
