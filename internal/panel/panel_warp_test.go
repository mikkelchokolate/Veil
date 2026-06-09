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

// TestPanelWarpToggleAppliesImmediately guards the fix for the WARP slider
// behaving like an inert checkbox: flipping it must save AND apply so WARP
// actually turns on/off (and the routing rule is added/removed) without the
// user hunting for a separate save button. Both the toggle change and the
// form submit route through a single commit that PUTs /api/warp then POSTs
// /api/apply to make the change live.
func TestPanelWarpToggleAppliesImmediately(t *testing.T) {
	actions := panelWarpActionsJS()
	for _, want := range []string{
		`async function applyWarpToggle()`,
		`async function commitWarp(`,
		`/api/apply`,
		`applyServices: true`,
		`applyLive: true`,
		`confirm: true`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("WARP actions missing %q", want)
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
