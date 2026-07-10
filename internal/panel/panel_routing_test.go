package panel

import (
	"strings"
	"testing"
)

func TestPanelRoutingActionsModuleRendersRuleAndPresetActions(t *testing.T) {
	actions := panelRoutingActionsJS()
	for _, want := range []string{
		`async function saveRoutingRule(event)`,
		`async function deleteRoutingRule()`,
		`async function applyRoutingPreset()`,
		`/api/routing/rules`,
		`/api/routing/presets/`,
		`routing-rule-name`,
		`routing-preset-profile`,
		`fetch('/api/warp', { headers: authHeaders() })`,
		`const saved = await loadJSON`,
		`if (!saved) return`,
		`const deleted = await loadJSON`,
		`if (!deleted) return`,
		`if (applied) setTimeout(loadRoutingRules, 800)`,
		`if (rules === null) return;`,
		`if (updated === null)`,
		`inputSwitch.checked = !requestedState;`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Routing actions missing %q", want)
		}
	}
}

func TestPanelRoutingCardModuleRendersRulesAndPresetControls(t *testing.T) {
	card := panelRoutingCardHTML()
	for _, want := range []string{
		`<h2>Routing rules</h2>`,
		`id="routing-rule-form"`,
		`id="routing-rule-name"`,
		`id="routing-rule-outbound"`,
		`id="routing-preset-profile"`,
		`id="apply-routing-preset"`,
		`id="routing-output"`,
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("Routing card missing %q", want)
		}
	}
}
