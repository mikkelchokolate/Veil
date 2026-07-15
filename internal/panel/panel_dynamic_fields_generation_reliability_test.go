package panel

import (
	"strings"
	"testing"
)

func TestDynamicProtocolFieldGenerationIsAbortableAndGenerationSafe(t *testing.T) {
	js := panelDynamicFieldsGenerationReliabilityJS()
	for _, want := range []string{
		`const protocolFieldGenerationControllers = new Map();`,
		`previousController.abort();`,
		`signal: controller.signal`,
		`sequence !== protocolFieldGenerationSequences.get(requestKey)`,
		`authElement && authElement.value !== provider`,
		`error.name === 'AbortError'`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("protocol field generation reliability missing %q", want)
		}
	}
}

func TestDynamicProtocolFieldGenerationSurfacesFailuresAndValidatesResponses(t *testing.T) {
	js := panelDynamicFieldsGenerationReliabilityJS()
	for _, want := range []string{
		`if (!response.ok) throw new Error(formatAPIError(text, response.status));`,
		`Room generation response is missing roomID.`,
		`veilShowInboundLocalError(`,
		`'protocolFields.' + String(key || '')`,
		`scheduleInboundValidation();`,
		`button.dataset.adminOnly = 'true';`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("protocol field generation error handling missing %q", want)
		}
	}
	if strings.Contains(js, `catch (_) {}`) {
		t.Fatal("protocol field generation must not silently discard request failures")
	}
}

func TestPanelCatalogMountsDynamicProtocolGenerationReliability(t *testing.T) {
	html := NewRenderer(NewSliceCatalog(nil).RenderSlots()).BaseHTML()
	if !strings.Contains(html, `const protocolFieldGenerationControllers = new Map();`) {
		t.Fatal("rendered Panel does not mount protocol field generation reliability")
	}
}
