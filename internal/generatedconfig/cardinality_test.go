package generatedconfig

import "testing"

func TestGeneratedConfigCardinalityRejectsMultipleEnabledSameProtocol(t *testing.T) {
	registry := NewProtocolRegistry([]Protocol{{Protocol: "naiveproxy", MaxEnabled: 1}})
	err := NewGeneratedConfigCardinality(Settings{}, registry).Validate([]Inbound{
		{Name: "a", Protocol: "naiveproxy", Enabled: true},
		{Name: "b", Protocol: "naiveproxy", Enabled: true},
	})
	if err == nil || err.Error() != "multiple enabled naiveproxy inbounds are not renderable as a single generated config yet" {
		t.Fatalf("err = %v", err)
	}
}

func TestGeneratedConfigCardinalityIgnoresDisabledAndProtocolsWithoutLimit(t *testing.T) {
	registry := NewProtocolRegistry([]Protocol{{Protocol: "naiveproxy", MaxEnabled: 1}, {Protocol: "mieru"}})
	err := NewGeneratedConfigCardinality(Settings{}, registry).Validate([]Inbound{
		{Name: "a", Protocol: "naiveproxy", Enabled: true},
		{Name: "b", Protocol: "mieru", Enabled: true},
		{Name: "c", Protocol: "naiveproxy", Enabled: false},
		{Name: "d", Protocol: "mieru", Enabled: true},
	})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
}
