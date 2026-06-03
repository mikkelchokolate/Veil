package panel

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/service"
)

func TestDiagnosticsCardsRenderToolControlsAndManagedLogUnits(t *testing.T) {
	runtimes := []service.ManagedRuntime{{Name: "veil", Unit: "veil.service"}, {Name: "mieru", Unit: "veil-mieru.service"}}
	cards := DiagnosticsCardsHTML(runtimes)
	for _, want := range []string{`<h2>Speedtest</h2>`, `id="run-speedtest"`, `<h2>DNS lookup</h2>`, `id="run-dns-lookup"`, `<h2>Ping</h2>`, `id="run-ping"`, `<h2>Firewall</h2>`, `id="load-firewall"`, `<h2>Service logs</h2>`, `id="load-logs"`, `value="veil-mieru"`, `mieru (veil-mieru.service)`} {
		want = strings.ReplaceAll(want, `\"`, `"`)
		if !strings.Contains(cards, want) {
			t.Fatalf("cards missing %q:\n%s", want, cards)
		}
	}
}

func TestDiagnosticsActionsRenderToolActions(t *testing.T) {
	actions := DiagnosticsActionsJS()
	for _, want := range []string{`run-speedtest`, `/api/tools/speedtest`, `load-logs`, `/api/logs?unit=`, `load-firewall`, `/api/firewall`, `run-dns-lookup`, `/api/tools/dns-lookup`, `run-ping`, `/api/tools/ping`} {
		if !strings.Contains(actions, want) {
			t.Fatalf("actions missing %q", want)
		}
	}
}
