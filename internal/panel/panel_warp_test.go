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
