package panel

import (
	"strings"
	"testing"
)

func TestPanelInboundModalClosesFromBackdrop(t *testing.T) {
	js := panelInboundControlsJS()
	for _, want := range []string{
		`const inboundModalOverlay = document.getElementById('inbound-modal-overlay');`,
		`inboundModalOverlay.addEventListener('click', (event) => {`,
		`if (event.target === inboundModalOverlay) closeInboundModal();`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("inbound modal backdrop handling missing %q", want)
		}
	}
}
