package api

import (
	"strings"
	"testing"
)

func TestPanelDiagnosticsActionsModuleRendersToolActions(t *testing.T) {
	actions := panelDiagnosticsActionsJS()
	for _, want := range []string{
		`run-speedtest`,
		`/api/tools/speedtest`,
		`load-logs`,
		`/api/logs?unit=`,
		`load-firewall`,
		`/api/firewall`,
		`run-dns-lookup`,
		`/api/tools/dns-lookup`,
		`run-ping`,
		`/api/tools/ping`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Diagnostics actions missing %q", want)
		}
	}
}

func TestPanelDiagnosticsCardsModuleRendersToolControls(t *testing.T) {
	cards := panelDiagnosticsCardsHTML()
	for _, want := range []string{
		`<h2>Speedtest</h2>`,
		`id="run-speedtest"`,
		`<h2>DNS lookup</h2>`,
		`id="run-dns-lookup"`,
		`<h2>Ping</h2>`,
		`id="run-ping"`,
		`<h2>Firewall</h2>`,
		`id="load-firewall"`,
		`<h2>Service logs</h2>`,
		`id="load-logs"`,
	} {
		if !strings.Contains(cards, want) {
			t.Fatalf("Diagnostics cards missing %q", want)
		}
	}
}
