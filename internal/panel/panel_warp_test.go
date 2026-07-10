package panel

import (
	"strings"
	"testing"
)

func TestPanelWarpActionsModuleRendersLoadAndSaveActions(t *testing.T) {
	actions := panelWarpActionsJS()
	for _, want := range []string{
		`async function loadWarpIntoForm()`,
		`async function saveWarpConfig(event)`,
		`/api/warp`,
		`warp-private-key`,
		`parseReserved`,
		`socksPort`,
		`mtu`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("WARP actions missing %q", want)
		}
	}
}

// TestPanelWarpToggleAppliesImmediately guards that flipping the WARP slider
// saves and applies the config immediately. Save and apply results remain
// separate so a failed live apply cannot make the UI lie about persisted state.
func TestPanelWarpToggleAppliesImmediately(t *testing.T) {
	actions := panelWarpActionsJS()
	for _, want := range []string{
		`async function applyWarpToggle()`,
		`async function commitWarp(`,
		`const applied = await loadJSON('/api/apply'`,
		`applyServices: true`,
		`applyLive: true`,
		`confirm: true`,
		`return { saved, applied: applied !== null };`,
		`return { saved: null, applied: false };`,
		`toggle.checked = Boolean(result.saved.enabled);`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("WARP actions missing %q", want)
		}
	}
}

func TestPanelWarpToggleOnlyRevertsWhenSaveFails(t *testing.T) {
	actions := panelWarpActionsJS()
	for _, want := range []string{
		`if (!result || !result.saved) {`,
		`toggle.checked = !enabled;`,
		`toggle.checked = Boolean(result.saved.enabled);`,
		`if (output) output.textContent = veilT('status.requestFailed'`,
		`if (toggle) { toggle.disabled = isViewerRole(); }`,
		`if (saveBtn) { saveBtn.disabled = isViewerRole(); }`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("WARP failure-state handling missing %q", want)
		}
	}
}

// TestPanelWarpToggleHasChangeBinding ensures the slider checkbox is wired to
// apply on flip, not just on a separate form submit.
func TestPanelWarpToggleHasChangeBinding(t *testing.T) {
	slice, ok := NewSliceCatalog(nil).Slice("warp")
	if !ok {
		t.Fatal("warp slice not found")
	}
	found := false
	for _, b := range slice.EventBindings {
		if b.ElementID == "warp-enabled" && b.Event == "change" && b.Handler == "applyWarpToggle" {
			found = true
		}
	}
	if !found {
		t.Fatalf("warp slice missing warp-enabled change->applyWarpToggle binding: %+v", slice.EventBindings)
	}
}

func TestPanelWarpCardModuleRendersRedactedWarpControls(t *testing.T) {
	card := panelWarpCardHTML()
	for _, want := range []string{
		`<h2>WARP</h2>`,
		`id="warp-form"`,
		`id="warp-private-key"`,
		`id="warp-license-key"`,
		`[REDACTED]`,
		`id="save-warp-config"`,
		`id="warp-output"`,
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("WARP card missing %q", want)
		}
	}
}
