package routing

import "testing"

func TestRoutingPresetApplicationCopiesPresetState(t *testing.T) {
	preset := RoutingPreset{Name: "ru", Source: RoutingSource{Repository: "repo"}, Rules: []RoutingRule{{Name: "rule"}}}
	state := RoutingPresetState{}
	NewRoutingPresetApplication(&state).Apply(preset)
	if state.ActivePreset != "ru" || state.Source.Repository != "repo" || len(state.Rules) != 1 || state.Rules[0].Name != "rule" {
		t.Fatalf("state = %+v", state)
	}
	preset.Rules[0].Name = "mutated"
	if state.Rules[0].Name != "rule" {
		t.Fatalf("rules not copied: %+v", state.Rules)
	}
}
