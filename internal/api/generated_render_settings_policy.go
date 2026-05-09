package api

import "github.com/veil-panel/veil/internal/generatedconfig"

type GeneratedRenderSettingsPolicy = generatedconfig.GeneratedRenderSettingsPolicy

func NewGeneratedRenderSettingsPolicy() GeneratedRenderSettingsPolicy {
	return generatedconfig.NewGeneratedRenderSettingsPolicy()
}

func hasRenderSettings(settings Settings) bool {
	return generatedconfig.NewGeneratedRenderSettingsPolicy().HasRenderSettings(settings)
}
