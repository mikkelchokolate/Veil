package routing

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestAliasTypesMatchModel(t *testing.T) {
	_ = Rule(model.RoutingRule{})
	_ = RoutingRule(model.RoutingRule{})
	_ = Source(model.RoutingSource{})
	_ = RoutingSource(model.RoutingSource{})
	_ = SourceFile(model.RoutingSourceFile{})
	_ = RoutingSourceFile(model.RoutingSourceFile{})
	_ = Preset(model.RoutingPreset{})
	_ = RoutingPreset(model.RoutingPreset{})
	_ = PresetResponse(model.RoutingPresetResponse{})
	_ = RoutingPresetResponse(model.RoutingPresetResponse{})
}

func TestPublicPresetProfilesMatchesRoutingPresetProfiles(t *testing.T) {
	if len(PresetProfiles()) != len(routingPresetProfiles()) {
		t.Fatal("PresetProfiles length mismatch")
	}
}

func TestPublicRouteDatSource(t *testing.T) {
	source := RouteDatSource()
	if source.Repository == "" {
		t.Fatal("RouteDatSource repository must not be empty")
	}
}

func TestPublicPresetByName(t *testing.T) {
	if _, ok := PresetByName("all"); !ok {
		t.Fatal("PresetByName should find 'all' preset")
	}
	if _, ok := PresetByName("nonexistent-preset-12345"); ok {
		t.Fatal("PresetByName should not find missing preset")
	}
}

func TestAliasConstructors(t *testing.T) {
	validator := NewRuleValidation()
	if err := validator.ValidateCreate(RoutingRule{Name: "rule", Match: "all", Outbound: "proxy"}); err != nil {
		t.Fatalf("NewRuleValidation constructor returned unusable validator: %v", err)
	}
	idx := NewRuleIndex([]Rule{{Name: "rule"}})
	if !idx.Has("rule") {
		t.Fatal("NewRuleIndex should index rules by name")
	}
}
