package api

import "github.com/veil-panel/veil/internal/generatedconfig"

type MieruGeneratedConfigModel = generatedconfig.MieruGeneratedConfigModel
type GeneratedMieruConfigRenderer = generatedconfig.GeneratedMieruConfigRenderer

func NewMieruGeneratedConfigModel(settings Settings) MieruGeneratedConfigModel {
	return generatedconfig.NewMieruGeneratedConfigModel(settings)
}

func NewGeneratedMieruConfigRenderer(settings Settings, paths GeneratedConfigPaths) GeneratedMieruConfigRenderer {
	return generatedconfig.NewGeneratedMieruConfigRenderer(settings, paths)
}
