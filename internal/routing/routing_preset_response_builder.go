package routing

type RoutingPresetResponseBuilder struct {
	activePreset string
	source       RoutingSource
	rules        []RoutingRule
	presets      []RoutingPreset
}

func NewRoutingPresetResponseBuilder(activePreset string, source RoutingSource, rules []RoutingRule) RoutingPresetResponseBuilder {
	return RoutingPresetResponseBuilder{activePreset: activePreset, source: source, rules: append([]RoutingRule(nil), rules...)}
}

func (b RoutingPresetResponseBuilder) WithPresets(presets []RoutingPreset) RoutingPresetResponseBuilder {
	b.presets = append([]RoutingPreset(nil), presets...)
	return b
}

func (b RoutingPresetResponseBuilder) Build() RoutingPresetResponse {
	return RoutingPresetResponse{
		ActivePreset: b.activePreset,
		Source:       b.source,
		Rules:        append([]RoutingRule(nil), b.rules...),
		Presets:      append([]RoutingPreset(nil), b.presets...),
	}
}
