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

func TestClientLinksActionsAreSerializedAndGenerationSafe(t *testing.T) {
	js := panelClientLinksReliabilityJS()
	for _, want := range []string{
		`let clientLinksActionInFlight = false;`,
		`let clientLinksOutputGeneration = 0;`,
		`if (clientLinksActionInFlight) return null;`,
		`const generation = ++clientLinksOutputGeneration;`,
		`if (generation !== clientLinksOutputGeneration) return;`,
		`setClientLinksActionControlsDisabled(true);`,
		`clientLinksActionInFlight = false;`,
		`loadClientLinks = async function()`,
		`loadClientSubscriptionPath = async function(path)`,
		`downloadClientLinksJSON = async function()`,
		`downloadClientConfigArtifacts = async function()`,
		`downloadClientSubscriptionPath = async function(path, filename)`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("client-links action reliability missing %q", want)
		}
	}
}

func TestClientLinksDownloadsValidateArtifactsAndDelayURLRevocation(t *testing.T) {
	js := panelClientLinksReliabilityJS()
	for _, want := range []string{
		`const artifacts = Array.isArray(body.artifacts) ? body.artifacts : [];`,
		`config = JSON.parse(artifact.content);`,
		`throw new Error('Invalid client config artifact '`,
		`setTimeout(() => URL.revokeObjectURL(url), 1000);`,
		`const text = await response.text();`,
		`if (!response.ok) throw new Error(formatAPIError(text, response.status));`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("client-links download validation missing %q", want)
		}
	}
}

func TestPanelCatalogMountsClientLinksReliabilityGuards(t *testing.T) {
	html := NewRenderer(NewSliceCatalog(nil).RenderSlots()).BaseHTML()
	for _, want := range []string{
		`let clientLinksModalRequestSequence = 0;`,
		`let clientLinksActionInFlight = false;`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered Panel does not mount client-links reliability guard %q", want)
		}
	}
}
