package generatedconfig

type ConfigValidationSpec = ValidationSpec

// ConfigValidationCatalog matches staged config paths against a supplied
// ArtifactCatalog. Callers that have access to the protocol registry should use
// NewArtifactCatalogFromRegistry so new protocol plugins are picked up
// automatically; the zero-value catalog is not usable.
type ConfigValidationCatalog struct {
	catalog ArtifactCatalog
}

// NewConfigValidationCatalog creates a validation catalog backed by the given
// ArtifactCatalog.
func NewConfigValidationCatalog(catalog ArtifactCatalog) ConfigValidationCatalog {
	return ConfigValidationCatalog{catalog: catalog}
}

func (c ConfigValidationCatalog) Match(path string) (ConfigValidationSpec, bool) {
	return c.catalog.ValidationSpec(path)
}
