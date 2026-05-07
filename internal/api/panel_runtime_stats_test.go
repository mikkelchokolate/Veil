package api

import (
	"strings"
	"testing"
)

func TestPanelRuntimeStatsActionsModuleRendersRuntimeLoadActions(t *testing.T) {
	actions := panelRuntimeStatsActionsJS()
	for _, want := range []string{
		`load-system-stats`,
		`/api/system`,
		`load-network-stats`,
		`/api/network`,
		`load-connections-stats`,
		`/api/connections`,
		`load-processes-stats`,
		`/api/processes`,
		`load-disk-stats`,
		`/api/disk`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Runtime stats actions missing %q", want)
		}
	}
}

func TestPanelRuntimeStatsCardsModuleRendersRuntimeControls(t *testing.T) {
	cards := panelRuntimeStatsCardsHTML()
	for _, want := range []string{
		`<h2>System resources</h2>`,
		`id="load-system-stats"`,
		`<h2>Network interfaces</h2>`,
		`id="load-network-stats"`,
		`<h2>Listening ports</h2>`,
		`id="load-connections-stats"`,
		`<h2>Managed processes</h2>`,
		`id="load-processes-stats"`,
		`<h2>Disk usage</h2>`,
		`id="load-disk-stats"`,
	} {
		if !strings.Contains(cards, want) {
			t.Fatalf("Runtime stats cards missing %q", want)
		}
	}
}
