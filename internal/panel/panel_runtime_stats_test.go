package panel

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
		`function finiteTelemetryNumber(value, fallback)`,
		`function appendTelemetryCell(row, text, className)`,
		`process.textContent = String(listener.process)`,
		`path.textContent = String(directory && directory.path || '')`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Runtime stats actions missing %q", want)
		}
	}
	for _, unsafe := range []string{
		`row.innerHTML`,
		`badge.innerHTML`,
		`dirCard.innerHTML`,
	} {
		if strings.Contains(actions, unsafe) {
			t.Fatalf("runtime host data must not be rendered through %q", unsafe)
		}
	}
}

// TestPanelTelemetryAutoRefreshesEverySecondByDefault guards that the system
// telemetry (CPU/mem/disk) refreshes once per second and that this auto-refresh
// is started automatically on page load, without the user clicking anything.
func TestPanelTelemetryAutoRefreshesEverySecondByDefault(t *testing.T) {
	actions := panelRuntimeStatsActionsJS()
	for _, want := range []string{
		`async function refreshSystemTelemetry`,
		`setInterval(refreshSystemTelemetry, 1000)`,
		`startTelemetryAutoRefresh()`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("telemetry auto-refresh actions missing %q", want)
		}
	}
	cards := panelRuntimeStatsCardsHTML()
	if !strings.Contains(cards, `id="toggle-telemetry-refresh"`) {
		t.Fatal("telemetry card missing auto-refresh toggle button")
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
