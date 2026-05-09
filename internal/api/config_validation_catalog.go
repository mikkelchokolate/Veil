package api

type ConfigValidationSpec struct {
	Name    string
	Config  string
	Command []string
}

type ConfigValidationCatalog struct{}

func NewConfigValidationCatalog() ConfigValidationCatalog { return ConfigValidationCatalog{} }

func (ConfigValidationCatalog) Match(path string) (ConfigValidationSpec, bool) {
	return NewGeneratedConfigArtifactCatalog().ValidationSpec(path)
}
