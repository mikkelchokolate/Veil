package panel

import (
	"strings"
	"testing"
)

func TestPanelTelemetrySkipsHiddenOrInactiveDashboard(t *testing.T) {
	js := panelRuntimeStatsVisibilityJS()
	for _, want := range []string{
		`const baseRefreshSystemTelemetry = refreshSystemTelemetry;`,
		`document.hidden || !dashboard || !dashboard.classList.contains('active')`,
		`return baseRefreshSystemTelemetry();`,
		`document.addEventListener('visibilitychange'`,
		`if (!document.hidden && telemetryRefreshInterval) refreshSystemTelemetry();`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("telemetry visibility guard missing %q", want)
		}
	}
}

func TestPanelRuntimeRawViewsUseBoundListeners(t *testing.T) {
	cards := panelRuntimeStatsCardsHTML()
	for _, want := range []string{
		`data-raw-output="system-stats-output"`,
		`data-raw-output="network-stats-output"`,
		`data-raw-output="connections-stats-output"`,
		`data-raw-output="processes-stats-output"`,
		`data-raw-output="disk-stats-output"`,
	} {
		if !strings.Contains(cards, want) {
			t.Fatalf("runtime cards missing raw-view binding %q", want)
		}
	}
	if strings.Contains(cards, `onclick=`) {
		t.Fatal("runtime stats cards must not use inline script handlers")
	}
	js := panelRuntimeStatsVisibilityJS()
	for _, want := range []string{
		`document.querySelectorAll('[data-raw-output]')`,
		`button.addEventListener('click'`,
		`toggleRawView(button.dataset.rawOutput)`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("runtime raw-view actions missing %q", want)
		}
	}
}

func TestPanelCatalogMountsTelemetryVisibilityGuard(t *testing.T) {
	html := NewRenderer(NewSliceCatalog(nil).RenderSlots()).BaseHTML()
	if !strings.Contains(html, `const baseRefreshSystemTelemetry = refreshSystemTelemetry;`) {
		t.Fatal("rendered Panel does not mount telemetry visibility guard")
	}
}
