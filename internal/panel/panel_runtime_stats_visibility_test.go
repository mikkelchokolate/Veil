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

func TestPanelCatalogMountsTelemetryVisibilityGuard(t *testing.T) {
	html := NewRenderer(NewSliceCatalog(nil).RenderSlots()).BaseHTML()
	if !strings.Contains(html, `const baseRefreshSystemTelemetry = refreshSystemTelemetry;`) {
		t.Fatal("rendered Panel does not mount telemetry visibility guard")
	}
}
