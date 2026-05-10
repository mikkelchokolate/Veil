package installer

import "github.com/veil-panel/veil/internal/hostenv"

type InstallPlanDefaults struct {
	currentPlatform func() hostenv.Platform
}

func NewInstallPlanDefaults(currentPlatform func() hostenv.Platform) InstallPlanDefaults {
	if currentPlatform == nil {
		currentPlatform = hostenv.CurrentPlatform
	}
	return InstallPlanDefaults{currentPlatform: currentPlatform}
}

func (d InstallPlanDefaults) Apply(input InstallPlanInput) InstallPlanInput {
	if input.Platform.OS == "" {
		input.Platform = d.currentPlatform()
	}
	return input
}
