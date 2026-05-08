package api

import "testing"

func TestStackProtocolPolicyIncludesMieruThroughPanel(t *testing.T) {
	if !NewStackProtocolPolicy("panel").Includes("mieru") {
		t.Fatal("Panel settings must not disable Mieru inbounds")
	}
}

func TestSettingsValidationMigratesMieruStackToPanel(t *testing.T) {
	settings := Settings{PanelListen: "127.0.0.1:2096", Stack: "mieru", Mode: "dev"}
	if err := NewSettingsValidation().NormalizeAndValidate(&settings, Settings{}); err != nil {
		t.Fatalf("legacy Mieru stack should migrate to panel: %v", err)
	}
	if settings.Stack != "panel" {
		t.Fatalf("Mieru must be configured as a Panel inbound, not persisted as stack %q", settings.Stack)
	}
}
