package generatedconfig

type GeneratedConfigCardinality struct {
	settings Settings
	registry ProtocolRegistry
}

func NewGeneratedConfigCardinality(settings Settings, registry ProtocolRegistry) GeneratedConfigCardinality {
	return GeneratedConfigCardinality{settings: settings, registry: registry}
}

func (c GeneratedConfigCardinality) Validate(inbounds []Inbound) error {
	return c.registry.Validate(c.settings, inbounds)
}
