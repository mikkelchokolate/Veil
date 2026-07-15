package protocols

type RenderSettingsPolicy interface {
	RequiresRenderSettings() bool
}

func RequiresRenderSettings(p ProtocolPlugin) bool {
	policy, ok := p.(RenderSettingsPolicy)
	if !ok {
		return true
	}
	return policy.RequiresRenderSettings()
}
