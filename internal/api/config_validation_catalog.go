package api

import "github.com/veil-panel/veil/internal/generatedconfig"

type ConfigValidationSpec = generatedconfig.ValidationSpec

type ConfigValidationCatalog struct{}

func NewConfigValidationCatalog() ConfigValidationCatalog { return ConfigValidationCatalog{} }

func (ConfigValidationCatalog) Match(path string) (ConfigValidationSpec, bool) {
	return NewGeneratedConfigArtifactCatalog().ValidationSpec(path)
}
