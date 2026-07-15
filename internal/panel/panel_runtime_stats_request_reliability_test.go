package panel

import (
	"strings"
	"testing"
)

func TestRuntimeStatsRequestsAreSerializedPerControl(t *testing.T) {
	js := panelRuntimeStatsRequestReliabilityJS()
	for _, want := range []string{
		`'system-stats-output': 'load-system-stats'`,
		`'network-stats-output': 'load-network-stats'`,
		`'connections-stats-output': 'load-connections-stats'`,
		`'processes-stats-output': 'load-processes-stats'`,
		`'disk-stats-output': 'load-disk-stats'`,
		`const baseLoadJSONForRuntimeStats = loadJSON;`,
		`if (!buttonID) return baseLoadJSONForRuntimeStats(path, outputId, options);`,
		`if (button.dataset.runtimeStatsInFlight === 'true') return null;`,
		`button.dataset.runtimeStatsInFlight = 'true';`,
		`button.disabled = true;`,
		`delete button.dataset.runtimeStatsInFlight;`,
		`if (!authResetPending) button.disabled = wasDisabled;`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("runtime stats request reliability missing %q", want)
		}
	}
}

func TestRenderedPanelMountsRuntimeStatsRequestLockOnce(t *testing.T) {
	html := NewRenderer(NewSliceCatalog(nil).RenderSlots()).BaseHTML()
	if got := strings.Count(html, `const baseLoadJSONForRuntimeStats = loadJSON;`); got != 1 {
		t.Fatalf("runtime stats request lock count = %d, want 1", got)
	}
}
