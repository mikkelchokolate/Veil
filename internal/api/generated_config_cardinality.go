package api

import "github.com/veil-panel/veil/internal/generatedconfig"

type GeneratedConfigCardinality struct {
	inner generatedconfig.GeneratedConfigCardinality
}

func NewGeneratedConfigCardinality(settings Settings) GeneratedConfigCardinality {
	return GeneratedConfigCardinality{inner: generatedconfig.NewGeneratedConfigCardinality(settings, NewGeneratedConfigProtocolRegistry().inner)}
}

func (c GeneratedConfigCardinality) Validate(inbounds []Inbound) error {
	return c.inner.Validate(inbounds)
}

func validateGeneratedConfigInboundCardinality(settings Settings, inbounds []Inbound) error {
	return NewGeneratedConfigCardinality(settings).Validate(inbounds)
}
