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

// #947: a struct copy of RoutingSource still shares the Files backing array —
// mutating the preset's file list after Apply must not leak into the applied
// state, and vice versa.
func TestRoutingPresetApplicationIsolatesSourceFiles(t *testing.T) {
	preset := RoutingPreset{
		Name: "ru",
		Source: RoutingSource{
			Repository: "repo",
			Files: []RoutingSourceFile{
				{Name: "geoip.dat", URL: "https://example.com/geoip.dat"},
				{Name: "geosite.dat", URL: "https://example.com/geosite.dat"},
			},
		},
	}
	state := RoutingPresetState{}
	NewRoutingPresetApplication(&state).Apply(preset)
	if len(state.Source.Files) != 2 || state.Source.Files[0].Name != "geoip.dat" {
		t.Fatalf("applied source = %+v", state.Source)
	}

	// Mutate the preset's list: the applied state must not observe it.
	preset.Source.Files[0].Name = "mutated.dat"
	preset.Source.Files = append(preset.Source.Files, RoutingSourceFile{Name: "extra.dat"})
	if state.Source.Files[0].Name != "geoip.dat" {
		t.Fatalf("preset mutation leaked into applied state: %+v", state.Source.Files)
	}
	if len(state.Source.Files) != 2 {
		t.Fatalf("preset append leaked into applied state: %+v", state.Source.Files)
	}

	// Mutate the applied state: the preset must not observe it either —
	// applying the same preset twice must be idempotent.
	state.Source.Files[1].URL = "mutated-url"
	NewRoutingPresetApplication(&state).Apply(preset)
	if state.Source.Files[1].URL != "https://example.com/geosite.dat" {
		t.Fatalf("state mutation leaked into preset, second apply not idempotent: %+v", state.Source.Files)
	}
}

func TestRoutingPresetApplicationNilStateIsNoop(t *testing.T) {
	app := NewRoutingPresetApplication(nil)
	app.Apply(RoutingPreset{Name: "ru", Rules: []RoutingRule{{Name: "rule"}}})
}
