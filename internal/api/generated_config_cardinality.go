package api

import "fmt"

type GeneratedConfigCardinality struct {
	settings Settings
}

func NewGeneratedConfigCardinality(settings Settings) GeneratedConfigCardinality {
	return GeneratedConfigCardinality{settings: settings}
}

func (c GeneratedConfigCardinality) Validate(inbounds []Inbound) error {
	counts := map[string]int{}
	for _, inbound := range inbounds {
		if !inbound.Enabled || !stackIncludesProtocol(c.settings.Stack, inbound.Protocol) {
			continue
		}
		switch inbound.Protocol {
		case "naiveproxy", "hysteria2":
			counts[inbound.Protocol]++
		}
	}
	for protocol, count := range counts {
		if count > 1 {
			return fmt.Errorf("multiple enabled %s inbounds are not renderable as a single generated config yet", protocol)
		}
	}
	return nil
}

func validateGeneratedConfigInboundCardinality(settings Settings, inbounds []Inbound) error {
	return NewGeneratedConfigCardinality(settings).Validate(inbounds)
}
