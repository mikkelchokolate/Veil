package installer

type InstallPlanDefaults struct {
	currentPlatform func() Platform
}

func NewInstallPlanDefaults(currentPlatform func() Platform) InstallPlanDefaults {
	if currentPlatform == nil {
		currentPlatform = CurrentPlatform
	}
	return InstallPlanDefaults{currentPlatform: currentPlatform}
}

func (d InstallPlanDefaults) Apply(input InstallPlanInput) InstallPlanInput {
	if input.Platform.OS == "" {
		input.Platform = d.currentPlatform()
	}
	if input.HysteriaVersion == "" {
		input.HysteriaVersion = "v2.6.0"
	}
	return input
}
