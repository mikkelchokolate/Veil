package panel

import (
	"strings"
	"testing"
)

func TestInboundReliableActionsRemoveMountTimeLoad(t *testing.T) {
	base := panelInboundActionsJS()
	if !strings.Contains(base, `setTimeout(loadInboundsIntoOutput, 500);`) {
		t.Fatal("base inbound actions no longer contain the legacy mount-time load; update the wrapper test")
	}
	actions := panelInboundReliableActionsJS()
	if strings.Contains(actions, `setTimeout(loadInboundsIntoOutput, 500);`) {
		t.Fatal("reliable inbound actions still contain the hidden mount-time load")
	}
	for _, want := range []string{
		`let inboundLoadGeneration = 0;`,
		`let inboundMutationInFlight = false;`,
		`const inboundModalOverlay = document.getElementById('inbound-modal-overlay');`,
	} {
		if got := strings.Count(actions, want); got != 1 {
			t.Fatalf("inbound runtime marker %q count = %d, want 1", want, got)
		}
	}
}

func TestRenderedPanelDoesNotAutoLoadInboundsOnMount(t *testing.T) {
	html := NewRenderer(NewSliceCatalog(nil).RenderSlots()).BaseHTML()
	if strings.Contains(html, `setTimeout(loadInboundsIntoOutput, 500);`) {
		t.Fatal("rendered Panel still auto-loads inbounds from a mount timer")
	}
	if got := strings.Count(html, `let inboundLoadGeneration = 0;`); got != 1 {
		t.Fatalf("rendered inbound reliability runtime count = %d, want 1", got)
	}
}
