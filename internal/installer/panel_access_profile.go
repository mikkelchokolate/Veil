package installer

import "github.com/veil-panel/veil/internal/panelaccess"

type PanelAccessProfileInput = panelaccess.ProfileInput
type PanelAccessProfileMaterial = panelaccess.ProfileMaterial
type PanelAccessProfile = panelaccess.Profile

func NewPanelAccessProfile(input PanelAccessProfileInput) PanelAccessProfile {
	return panelaccess.NewProfile(input)
}
