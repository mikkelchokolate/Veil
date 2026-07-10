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
		`if (!saved) return null`,
		`const deleted = await loadJSON`,
		`if (!deleted) return null`,
		`await loadRoutingRules();`,
		`rules === null`,
		`if (updated === null)`,
		`inputSwitch.checked = !requestedState;`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Routing actions missing %q", want)
		}
	}
	if strings.Contains(actions, `setTimeout(loadRoutingRules, 800)`) {
		t.Fatal("routing preset refresh must wait for the completed request instead of an arbitrary timer")
	}
}

func TestPanelRoutingGuardsStaleModalAndLoadResponses(t *testing.T) {
	actions := panelRoutingActionsJS()
	for _, want := range []string{
		`let routingModalGeneration = 0;`,
		`const generation = ++routingModalGeneration;`,
		`if (generation !== routingModalGeneration) return;`,
		`routingModalGeneration += 1;`,
		`let routingRulesLoadGeneration = 0;`,
		`if (generation !== routingRulesLoadGeneration || rules === null) return;`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("routing stale-response guard missing %q", want)
		}
	}
}

func TestPanelRoutingSerializesMutations(t *testing.T) {
	actions := panelRoutingActionsJS()
	for _, want := range []string{
		`let routingMutationInFlight = false;`,
		`async function withRoutingMutation(action)`,
		`if (routingMutationInFlight) return null;`,
		`routingMutationInFlight = true;`,
		`return await action();`,
		`routingMutationInFlight = false;`,
		`setRoutingMutationControlsDisabled(false);`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("routing mutation lock missing %q", want)
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
