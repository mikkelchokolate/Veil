package legacy

import "testing"

func TestIsPanelOnlyStackAcceptsOnlyEmptyOrPanel(t *testing.T) {
	for _, value := range []string{"", "panel"} {
		if !IsPanelOnlyStack(value) {
			t.Fatalf("expected %q to be accepted", value)
		}
	}
	for _, value := range []string{"both", "naive", "hysteria2", "mieru", " panel "} {
		if IsPanelOnlyStack(value) {
			t.Fatalf("expected %q to be rejected", value)
		}
	}
}

func TestIsTrimmedPanelOnlyStackSupportsCLIWhitespace(t *testing.T) {
	if !IsTrimmedPanelOnlyStack(" panel ") {
		t.Fatalf("trimmed CLI stack should accept panel")
	}
	if IsTrimmedPanelOnlyStack("mieru") {
		t.Fatalf("trimmed CLI stack should reject protocol stack")
	}
}

func TestNormalizeSettingsStackAcceptsLegacySettingsValues(t *testing.T) {
	for _, value := range []string{"", "panel", "both", "naive", "hysteria2", "mieru"} {
		got, ok := NormalizeSettingsStack(value)
		if !ok || got != "panel" {
			t.Fatalf("NormalizeSettingsStack(%q) = %q, %v", value, got, ok)
		}
	}
	for _, value := range []string{"bad", " ", "BOTH"} {
		if got, ok := NormalizeSettingsStack(value); ok || got != "" {
			t.Fatalf("NormalizeSettingsStack(%q) = %q, %v", value, got, ok)
		}
	}
}
