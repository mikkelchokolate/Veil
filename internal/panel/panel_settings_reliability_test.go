package panel

import (
	"strings"
	"testing"
)

func TestPanelSettingsReliabilityGuardsStaleLoads(t *testing.T) {
	js := panelSettingsReliabilityJS()
	for _, want := range []string{
		`let settingsLoadGeneration = 0;`,
		`const generation = ++settingsLoadGeneration;`,
		`if (generation !== settingsLoadGeneration) return null;`,
		`if (generation !== settingsLoadGeneration) return false;`,
		`window.cachedSettings = data;`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("settings stale-load guard missing %q", want)
		}
	}
}

func TestPanelSettingsReliabilitySerializesSaves(t *testing.T) {
	js := panelSettingsReliabilityJS()
	for _, want := range []string{
		`let settingsSaveInFlight = false;`,
		`if (!form || settingsSaveInFlight) return null;`,
		`settingsSaveInFlight = true;`,
		`setSettingsControlsDisabled(true);`,
		`settingsSaveInFlight = false;`,
		`setSettingsControlsDisabled(false);`,
		`form.reportValidity();`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("settings save lock missing %q", want)
		}
	}
}

func TestPanelSettingsReliabilityValidatesResponseShape(t *testing.T) {
	js := panelSettingsReliabilityJS()
	for _, want := range []string{
		`if (!data || typeof data !== 'object' || Array.isArray(data))`,
		`if (!saved || typeof saved !== 'object' || Array.isArray(saved))`,
		`formatAPIError(text, response.status)`,
		`await ensureProtocolSchemas();`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("settings response validation missing %q", want)
		}
	}
}

func TestPanelCatalogMountsSettingsReliabilityGuard(t *testing.T) {
	html := NewRenderer(NewSliceCatalog(nil).RenderSlots()).BaseHTML()
	if !strings.Contains(html, `let settingsSaveInFlight = false;`) {
		t.Fatal("rendered Panel does not mount settings reliability guard")
	}
}
