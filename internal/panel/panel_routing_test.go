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
	card := panelRoutingHardenedCardHTML()
	for _, want := range []string{
		`<h2>Routing rules</h2>`,
		`id="add-routing-rule-btn"`,
		`id="load-routing-rules"`,
		`id="clear-routing-output"`,
		`id="close-routing-modal"`,
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
	if strings.Contains(card, `onclick=`) {
		t.Fatal("rendered routing card must not use inline event handlers")
	}
}

func TestPanelRoutingControlsUseBoundListeners(t *testing.T) {
	js := panelRoutingControlsJS()
	for _, want := range []string{
		`document.getElementById('add-routing-rule-btn').addEventListener('click', () => openRoutingModal(null));`,
		`document.getElementById('load-routing-rules').addEventListener('click', loadRoutingRules);`,
		`document.getElementById('clear-routing-output').addEventListener('click'`,
		`document.getElementById('close-routing-modal').addEventListener('click', closeRoutingModal);`,
		`document.getElementById('routing-modal').addEventListener('click'`,
		`event.target === event.currentTarget`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("routing control binding missing %q", want)
		}
	}
}

func TestPanelCatalogMountsHardenedRoutingControls(t *testing.T) {
	html := NewRenderer(NewSliceCatalog(nil).RenderSlots()).BaseHTML()
	for _, want := range []string{
		`id="load-routing-rules"`,
		`id="clear-routing-output"`,
		`id="close-routing-modal"`,
		`document.getElementById('routing-modal').addEventListener('click'`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered Panel does not mount hardened routing control %q", want)
		}
	}
	for _, forbidden := range []string{
		`onclick="openRoutingModal(null)"`,
		`onclick="loadRoutingRules()"`,
		`onclick="if(event.target === this) closeRoutingModal()"`,
	} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("rendered Panel still contains routing inline handler %q", forbidden)
		}
	}
}
