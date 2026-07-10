package panel

import (
	"strings"
	"testing"
)

func TestClientLinksModalCancelsStaleRequests(t *testing.T) {
	js := panelClientLinksReliabilityJS()
	for _, want := range []string{
		`let clientLinksModalRequestSequence = 0;`,
		`let clientLinksModalController = null;`,
		`clientLinksModalController.abort();`,
		`signal: controller.signal`,
		`sequence !== clientLinksModalRequestSequence`,
		`error.name === 'AbortError'`,
		`clearClientLinksModalQRs();`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("client-links modal reliability guard missing %q", want)
		}
	}
}

func TestClientLinksQRCancelsHiddenRenders(t *testing.T) {
	js := panelClientLinksReliabilityJS()
	for _, want := range []string{
		`const clientLinkQRControllers = new Map();`,
		`existingController.abort();`,
		`clientLinkQRControllers.get(qrId) !== controller`,
		`container.style.display === 'none'`,
		`URL.revokeObjectURL(container.dataset.objectUrl)`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("client-links QR reliability guard missing %q", want)
		}
	}
}

func TestClientLinksClipboardRejectionIsAwaited(t *testing.T) {
	js := panelClientLinksReliabilityJS()
	for _, want := range []string{
		`window.copyModalLink = async function`,
		`await navigator.clipboard.writeText(input.value);`,
		`catch (error)`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("clipboard error handling missing %q", want)
		}
	}
}

func TestPanelCatalogMountsClientLinksReliabilityGuards(t *testing.T) {
	html := NewRenderer(NewSliceCatalog(nil).RenderSlots()).BaseHTML()
	if !strings.Contains(html, `let clientLinksModalRequestSequence = 0;`) {
		t.Fatal("rendered Panel does not mount client-links reliability guards")
	}
}
