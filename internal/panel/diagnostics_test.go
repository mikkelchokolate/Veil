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

func TestManagedLogUnitOptionsEscapeRuntimeDerivedHTML(t *testing.T) {
	runtimes := []service.ManagedRuntime{{
		Name: `hysteria2-\"><img src=x onerror=alert(1)>`,
		Unit: `veil-hysteria2@\"><svg onload=alert(1)>.service`,
	}}
	// Remove the Go/JSON transport escapes so the renderer receives the exact
	// quote-and-tag payload that would break an option attribute without escaping.
	runtimes[0].Name = strings.ReplaceAll(runtimes[0].Name, `\"`, `"`)
	runtimes[0].Unit = strings.ReplaceAll(runtimes[0].Unit, `\"`, `"`)
	options := ManagedLogUnitOptionsHTML(runtimes)

	for _, unsafe := range []string{`<img`, `<svg`, `value="veil-hysteria2@">`} {
		unsafe = strings.ReplaceAll(unsafe, `\"`, `"`)
		if strings.Contains(options, unsafe) {
			t.Fatalf("diagnostic runtime option contains unsafe fragment %q: %s", unsafe, options)
		}
	}
	for _, want := range []string{`&lt;img`, `&lt;svg`, `&#34;&gt;`} {
		if !strings.Contains(options, want) {
			t.Fatalf("diagnostic runtime option missing escaped fragment %q: %s", want, options)
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

func TestDiagnosticsSerializeExpensiveRequestsAndValidateCounts(t *testing.T) {
	actions := DiagnosticsActionsJS()
	for _, want := range []string{
		`async function runDiagnosticAction(buttonID, action)`,
		`button.dataset.diagnosticInFlight === 'true'`,
		`button.disabled = true;`,
		`return await action();`,
		`delete button.dataset.diagnosticInFlight;`,
		`function diagnosticIntegerValue(id, fallback)`,
		`input.checkValidity()`,
		`Number.isInteger(value)`,
		`runDiagnosticAction('run-speedtest'`,
		`runDiagnosticAction('load-logs'`,
		`runDiagnosticAction('run-dns-lookup'`,
		`runDiagnosticAction('run-ping'`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("diagnostic request reliability missing %q", want)
		}
	}
}

func TestDiagnosticsUseLoadJSONResultInsteadOfParsingRenderedOutput(t *testing.T) {
	actions := DiagnosticsActionsJS()
	for _, want := range []string{
		`const data = await loadJSON('/api/logs?unit='`,
		`if (data && data.output)`,
		`document.getElementById('logs-output').textContent = data.output;`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("diagnostic log rendering missing %q", want)
		}
	}
	if strings.Contains(actions, `JSON.parse(el.textContent)`) {
		t.Fatal("diagnostic logs must use the request result rather than reparsing UI text")
	}
}
