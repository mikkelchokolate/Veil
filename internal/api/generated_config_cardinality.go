package api

type GeneratedConfigCardinality struct {
	settings Settings
}

func NewGeneratedConfigCardinality(settings Settings) GeneratedConfigCardinality {
	return GeneratedConfigCardinality{settings: settings}
}

func (c GeneratedConfigCardinality) Validate(inbounds []Inbound) error {
	return NewGeneratedConfigProtocolRegistry().Validate(c.settings, inbounds)
}

func validateGeneratedConfigInboundCardinality(settings Settings, inbounds []Inbound) error {
	return NewGeneratedConfigCardinality(settings).Validate(inbounds)
}
