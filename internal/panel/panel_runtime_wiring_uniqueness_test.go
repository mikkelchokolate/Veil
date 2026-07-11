package panel

import (
	"strings"
	"testing"
)

func TestPanelReliabilityRuntimesAreMountedExactlyOnce(t *testing.T) {
	html := NewRenderer(NewSliceCatalog(nil).RenderSlots()).BaseHTML()
	for _, marker := range []string{
		`let veilEditingInboundName = '';`,
		`const veilBaseEnsureProtocolSchemas = ensureProtocolSchemas;`,
		`const baseLoadJSONForRuntimeStats = loadJSON;`,
	} {
		if count := strings.Count(html, marker); count != 1 {
			t.Fatalf("rendered Panel contains %d copies of reliability runtime %q, want exactly one", count, marker)
		}
	}
}

func TestClientProfileModuleDoesNotInjectCrossCuttingRuntimes(t *testing.T) {
	js := panelClientProfileActionsJS()
	for _, unwanted := range []string{
		`let veilEditingInboundName = '';`,
		`const veilBaseEnsureProtocolSchemas = ensureProtocolSchemas;`,
		`loadJSON = async function(path, outputId, options)`,
		`panelRequestReliabilityJS`,
	} {
		if strings.Contains(js, unwanted) {
			t.Fatalf("client profile actions unexpectedly contain cross-cutting runtime %q", unwanted)
		}
	}
}
