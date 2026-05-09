package api

import "github.com/veil-panel/veil/internal/generatedconfig"

type GeneratedConfigPaths = generatedconfig.Paths

func NewGeneratedConfigPaths(applyRoot string) GeneratedConfigPaths {
	return generatedconfig.NewPaths(applyRoot)
}
