package api

import "testing"

func TestGeneratedConfigCardinalityRejectsMultipleEnabledSameProtocol(t *testing.T) {
	err := NewGeneratedConfigCardinality(Settings{Stack: "both"}).Validate([]Inbound{
		{Name: "a", Protocol: "naiveproxy", Enabled: true},
		{Name: "b", Protocol: "naiveproxy", Enabled: true},
	})
	if err == nil || err.Error() != "multiple enabled naiveproxy inbounds are not renderable as a single generated config yet" {
		t.Fatalf("err = %v", err)
	}
}

func TestGeneratedConfigCardinalityIgnoresDisabledAndExcludedStackProtocols(t *testing.T) {
	err := NewGeneratedConfigCardinality(Settings{Stack: "naive"}).Validate([]Inbound{
		{Name: "a", Protocol: "naiveproxy", Enabled: true},
		{Name: "b", Protocol: "hysteria2", Enabled: true},
		{Name: "c", Protocol: "naiveproxy", Enabled: false},
	})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
}
