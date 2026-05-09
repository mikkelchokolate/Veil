package api

import "github.com/veil-panel/veil/internal/routing"

var (
	ErrRoutingRuleInvalid       = routing.ErrRoutingRuleInvalid
	ErrRoutingRuleNotFound      = routing.ErrRoutingRuleNotFound
	ErrRoutingRuleDuplicateName = routing.ErrRoutingRuleDuplicateName
)

type RoutingRuleValidation = routing.RoutingRuleValidation
type RoutingRuleIndex = routing.RoutingRuleIndex
type RoutingPresetState = routing.RoutingPresetState
type RoutingPresetApplication = routing.RoutingPresetApplication
type RoutingPresetResponseBuilder = routing.RoutingPresetResponseBuilder

func NewRoutingRuleValidation() RoutingRuleValidation { return routing.NewRoutingRuleValidation() }
func NewRoutingRuleIndex(rules []RoutingRule) RoutingRuleIndex {
	return routing.NewRoutingRuleIndex(rules)
}
func NewRoutingPresetApplication(state *RoutingPresetState) RoutingPresetApplication {
	return routing.NewRoutingPresetApplication(state)
}
func NewRoutingPresetResponseBuilder(activePreset string, source RoutingSource, rules []RoutingRule) RoutingPresetResponseBuilder {
	return routing.NewRoutingPresetResponseBuilder(activePreset, source, rules)
}

func routeDatSource() RoutingSource                         { return routing.RouteDatSource() }
func routingPresetProfiles() []RoutingPreset                { return routing.PresetProfiles() }
func routingPresetByName(name string) (RoutingPreset, bool) { return routing.PresetByName(name) }
