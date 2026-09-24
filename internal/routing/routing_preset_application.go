package routing

type RoutingPresetState struct {
	ActivePreset string
	Source       RoutingSource
	Rules        []RoutingRule
}

type RoutingPresetApplication struct {
	state *RoutingPresetState
}

func NewRoutingPresetApplication(state *RoutingPresetState) RoutingPresetApplication {
	return RoutingPresetApplication{state: state}
}

func (a RoutingPresetApplication) Apply(preset RoutingPreset) {
	if a.state == nil {
		return
	}
	a.state.ActivePreset = preset.Name
	a.state.Source = preset.Source
	// Deep-copy the source file list: a struct copy still shares the Files
	// backing array, so a later mutation of the preset would leak into the
	// applied state (#947).
	a.state.Source.Files = append([]RoutingSourceFile(nil), preset.Source.Files...)
	a.state.Rules = append([]RoutingRule(nil), preset.Rules...)
}
