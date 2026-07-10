package panel

import (
	"strings"
	"testing"
)

func TestPanelCatalogMountsSharedAndInboundReliabilityGuards(t *testing.T) {
	html := NewRenderer(NewSliceCatalog(nil).RenderSlots()).BaseHTML()
	for _, want := range []string{
		`loadJSON = async function(path, outputId, options)`,
		`requestHeaders(Object.assign({}, requestOptions.headers || {}))`,
		`let veilEditingInboundName = ''`,
		`window.protocolSchemaPromise = null;`,
		`window.cachedInbounds.some((inbound) => inbound.name === name)`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered Panel does not mount reliability guard %q", want)
		}
	}
}

func TestPanelSettingsRefreshRemainsAvailableToViewers(t *testing.T) {
	js := panelSettingsReliabilityJS()
	for _, want := range []string{
		`if (saveButton) saveButton.disabled = Boolean(disabled) || isViewerRole();`,
		`if (loadButton) loadButton.disabled = Boolean(disabled);`,
		`if (loadButton && generation === settingsLoadGeneration) loadButton.disabled = false;`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("settings reliability missing viewer-safe refresh behavior %q", want)
		}
	}
	if strings.Contains(js, `loadButton.disabled = isViewerRole()`) {
		t.Fatal("settings refresh must not be disabled for viewer accounts")
	}
}
