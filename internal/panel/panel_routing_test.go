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
		`const saved = await withRoutingMutation`,
		`if (!saved) return`,
		`const deleted = await withRoutingMutation`,
		`if (!deleted) return`,
		`await loadRoutingRules();`,
		`inputSwitch.checked = !requestedState;`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Routing actions missing %q", want)
		}
	}
	if strings.Contains(actions, `setTimeout(loadRoutingRules`) {
		t.Fatal("routing rules must load from tab activation or explicit refresh, not a mount-time timer")
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
		`let routingRulesLoadController = null;`,
		`routingRulesLoadController.abort();`,
		`signal: controller.signal`,
		`generation !== routingRulesLoadGeneration || controller.signal.aborted`,
		`error.name === 'AbortError'`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("routing stale-response guard missing %q", want)
		}
	}
}

func TestPanelRoutingSerializesAllMutations(t *testing.T) {
	actions := panelRoutingActionsJS()
	for _, want := range []string{
		`let routingMutationInFlight = false;`,
		`async function withRoutingMutation(action)`,
		`if (routingMutationInFlight) return null;`,
		`cancelRoutingRulesLoad();`,
		`routingMutationInFlight = true;`,
		`return await action();`,
		`routingMutationInFlight = false;`,
		`setRoutingMutationControlsDisabled(false);`,
		`inputSwitch.dataset.routingMutation = 'true';`,
		`btnEdit.dataset.routingMutation = 'true';`,
		`const updated = await withRoutingMutation`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("routing mutation lock missing %q", want)
		}
	}
}

func TestPanelRoutingLoadsDoNotOverwriteOutputAfterMutation(t *testing.T) {
	actions := panelRoutingActionsJS()
	for _, want := range []string{
		`if (routingMutationInFlight) return null;`,
		`const response = await fetch('/api/routing/rules'`,
		`if (generation !== routingRulesLoadGeneration || controller.signal.aborted) return null;`,
		`if (!Array.isArray(rules)) throw new Error('Invalid routing rules response.');`,
		`if (output) output.textContent = JSON.stringify(rules, null, 2);`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("routing load reliability missing %q", want)
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
		`let routingRulesLoadController = null;`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered Panel does not mount hardened routing control %q", want)
		}
	}
	for _, forbidden := range []string{
		`onclick="openRoutingModal(null)"`,
		`onclick="loadRoutingRules()"`,
		`onclick="if(event.target === this) closeRoutingModal()"`,
		`setTimeout(loadRoutingRules`,
	} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("rendered Panel still contains routing legacy behavior %q", forbidden)
		}
	}
}
