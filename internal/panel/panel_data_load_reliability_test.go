package panel

import (
	"strings"
	"testing"
)

func TestSharedDataLoadOutputsAreSerialized(t *testing.T) {
	js := panelDataLoadRequestReliabilityJS()
	for _, want := range []string{
		`'routing-output': '[data-load][data-output="routing-output"]'`,
		`const baseLoadJSONForDataLoadControls = loadJSON;`,
		`if (!selector) return baseLoadJSONForDataLoadControls(path, outputId, options);`,
		`control.dataset.dataLoadInFlight === 'true'`,
		`control.dataset.dataLoadInFlight = 'true';`,
		`control.disabled = true;`,
		`delete control.dataset.dataLoadInFlight;`,
		`control.disabled = previousDisabled[index];`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("data-load request reliability missing %q", want)
		}
	}
}

func TestRenderedPanelMountsDataLoadLockOnce(t *testing.T) {
	html := NewRenderer(NewSliceCatalog(nil).RenderSlots()).BaseHTML()
	if got := strings.Count(html, `const baseLoadJSONForDataLoadControls = loadJSON;`); got != 1 {
		t.Fatalf("data-load request lock count = %d, want 1", got)
	}
}
