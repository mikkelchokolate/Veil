package panel

import (
	"strings"
	"testing"
)

func TestPanelWarpLoadsAreAbortableAndGenerationSafe(t *testing.T) {
	js := panelWarpLoadReliabilityJS()
	for _, want := range []string{
		`let warpLoadGeneration = 0;`,
		`let warpLoadController = null;`,
		`warpLoadController.abort();`,
		`if (warpCommitInFlight) return null;`,
		`signal: controller.signal`,
		`generation !== warpLoadGeneration || controller.signal.aborted`,
		`error.name === 'AbortError'`,
		`warpFormForLoadCancellation.addEventListener('input', cancelWarpLoad);`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("WARP load reliability missing %q", want)
		}
	}
}

func TestPanelWarpCommitInvalidatesOutstandingLoads(t *testing.T) {
	js := panelWarpLoadReliabilityJS()
	for _, want := range []string{
		`const veilBaseCommitWarp = commitWarp;`,
		`commitWarp = async function(enabled)`,
		`cancelWarpLoad();`,
		`return await veilBaseCommitWarp(enabled);`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("WARP commit/load coordination missing %q", want)
		}
	}
}

func TestRenderedPanelDoesNotAutoLoadWarpOnMount(t *testing.T) {
	actions := panelWarpReliableActionsJS()
	if strings.Contains(actions, `setTimeout(loadWarpIntoForm, 150)`) {
		t.Fatal("rendered WARP actions still auto-load on page mount")
	}
	html := NewRenderer(NewSliceCatalog(nil).RenderSlots()).BaseHTML()
	for _, want := range []string{
		`let warpLoadGeneration = 0;`,
		`loadWarpIntoForm = async function()`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered Panel does not mount WARP reliability guard %q", want)
		}
	}
	if strings.Contains(html, `setTimeout(loadWarpIntoForm, 150)`) {
		t.Fatal("rendered Panel still contains mount-time WARP load")
	}
}
