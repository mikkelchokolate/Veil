package routing

import "testing"

func TestRoutingPresetResponseBuilderBuildsResponseAndCopiesRules(t *testing.T) {
	rules := []RoutingRule{{Name: "rule"}}
	response := NewRoutingPresetResponseBuilder("ru", RoutingSource{Repository: "repo"}, rules).WithPresets([]RoutingPreset{{Name: "ru"}}).Build()
	if response.ActivePreset != "ru" || response.Source.Repository != "repo" || len(response.Rules) != 1 || len(response.Presets) != 1 {
		t.Fatalf("response = %+v", response)
	}
	rules[0].Name = "mutated"
	if response.Rules[0].Name != "rule" {
		t.Fatalf("rules not copied: %+v", response.Rules)
	}
}
