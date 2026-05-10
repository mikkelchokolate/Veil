package api

import "github.com/veil-panel/veil/internal/generatedconfig"

func validateGeneratedConfigInboundCardinality(settings Settings, inbounds []Inbound) error {
	return generatedconfig.NewGeneratedConfigCardinality(settings, NewGeneratedConfigProtocolRegistry().inner).Validate(inbounds)
}
