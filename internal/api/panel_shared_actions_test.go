package api

import (
	"strings"
	"testing"
)

func TestPanelUtilityActionsModuleRendersSharedHelpers(t *testing.T) {
	actions := panelUtilityActionsJS()
	for _, want := range []string{
		`function parseReserved(value)`,
		`function numberOrZero(id)`,
		`Number.isInteger`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Utility actions missing %q", want)
		}
	}
}

func TestPanelEventBindingsModuleRendersCrossModuleBindings(t *testing.T) {
	bindings := panelEventBindingsJS()
	for _, want := range []string{
		`document.querySelectorAll('[data-load]')`,
		`settings-form`,
		`load-client-links`,
		`inbound-form`,
		`routing-rule-form`,
		`warp-form`,
		`loadSettingsIntoForm();`,
		`loadServiceStatus();`,
	} {
		if !strings.Contains(bindings, want) {
			t.Fatalf("Event bindings missing %q", want)
		}
	}
}
