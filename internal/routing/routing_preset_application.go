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
	a.state.Rules = append([]RoutingRule(nil), preset.Rules...)
}
