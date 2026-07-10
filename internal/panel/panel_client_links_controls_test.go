package panel

import (
	"strings"
	"testing"
)

func TestPanelClientLinksModalControlsAvoidInlineHandlers(t *testing.T) {
	html := NewRenderer(NewSliceCatalog(nil).RenderSlots()).BaseHTML()
	for _, want := range []string{
		`id="close-client-links-modal"`,
		`id="close-client-links-modal-footer"`,
		`clientLinksModalOverlay.addEventListener('click'`,
		`if (event.target === clientLinksModalOverlay) closeClientLinksModal();`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered Panel missing client-links modal control %q", want)
		}
	}
	if strings.Contains(html, `onclick="closeClientLinksModal()"`) {
		t.Fatal("rendered Panel still contains inline client-links modal handlers")
	}
}
