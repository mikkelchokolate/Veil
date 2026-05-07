package api

type GeneratedRenderSettingsPolicy struct{}

func NewGeneratedRenderSettingsPolicy() GeneratedRenderSettingsPolicy {
	return GeneratedRenderSettingsPolicy{}
}

func (GeneratedRenderSettingsPolicy) HasRenderSettings(settings Settings) bool {
	return settings.Domain != "" ||
		settings.Email != "" ||
		settings.NaiveUsername != "" ||
		settings.NaivePassword != "" ||
		settings.Hysteria2Password != "" ||
		settings.MasqueradeURL != "" ||
		settings.FallbackRoot != ""
}

func hasRenderSettings(settings Settings) bool {
	return NewGeneratedRenderSettingsPolicy().HasRenderSettings(settings)
}
