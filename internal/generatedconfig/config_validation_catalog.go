package generatedconfig

type ConfigValidationSpec = ValidationSpec

type ConfigValidationCatalog struct{}

func NewConfigValidationCatalog() ConfigValidationCatalog { return ConfigValidationCatalog{} }

func (ConfigValidationCatalog) Match(path string) (ConfigValidationSpec, bool) {
	return NewDefaultArtifactCatalog().ValidationSpec(path)
}
