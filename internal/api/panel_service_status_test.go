package api

import (
	"strings"
	"testing"
)

func TestPanelServiceStatusCardModuleRendersOperationalControls(t *testing.T) {
	card := panelServiceStatusCardHTML()
	for _, want := range []string{
		`<h2>Service status</h2>`,
		`id="load-service-status"`,
		`id="toggle-auto-refresh"`,
		`id="service-status-output"`,
		`id="restart-veil"`,
		`id="restart-caddy"`,
		`id="restart-hysteria2"`,
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("Service status card missing %q", want)
		}
	}
}

func TestPanelServiceRestartActionsModuleRendersRestartActions(t *testing.T) {
	actions := panelServiceRestartActionsJS()
	for _, want := range []string{
		`restart-veil`,
		`/api/services/veil/restart`,
		`restart-caddy`,
		`/api/services/caddy/restart`,
		`restart-hysteria2`,
		`/api/services/hysteria2/restart`,
		`confirm: true`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Service restart actions missing %q", want)
		}
	}
}

func TestPanelServiceStatusActionsModuleRendersAutoRefreshActions(t *testing.T) {
	actions := panelServiceStatusActionsJS()
	for _, want := range []string{
		`function loadServiceStatus()`,
		`let autoRefreshInterval = null`,
		`toggle-auto-refresh`,
		`setInterval(loadServiceStatus, 10000)`,
		`beforeunload`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Service status actions missing %q", want)
		}
	}
}
