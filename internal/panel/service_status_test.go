package panel

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/service"
)

func TestServiceStatusCardRendersRestartControls(t *testing.T) {
	runtimes := []service.ManagedRuntime{{ActionName: "veil", ManualRestart: true}, {ActionName: "mieru", ManualRestart: true}, {ActionName: "hidden", ManualRestart: false}}
	card := ServiceStatusCardHTML(runtimes)
	for _, want := range []string{`<h2>Service status</h2>`, `id="load-service-status"`, `id="toggle-auto-refresh"`, `id="restart-veil"`, `id="restart-mieru"`} {
		if !strings.Contains(card, strings.ReplaceAll(want, `\"`, `"`)) {
			t.Fatalf("card missing %q:\n%s", want, card)
		}
	}
	if strings.Contains(card, `restart-hidden`) {
		t.Fatalf("card should not include non-manual runtime:\n%s", card)
	}
}

func TestServiceStatusRefreshesAreSingleFlightAndVisibilityAware(t *testing.T) {
	actions := ServiceStatusActionsJS()
	for _, want := range []string{
		`let serviceStatusLoadInFlight = false;`,
		`async function loadServiceStatus()`,
		`if (serviceStatusLoadInFlight) return null;`,
		`serviceStatusLoadInFlight = true;`,
		`serviceStatusLoadInFlight = false;`,
		`if (document.hidden || !dashboard || !dashboard.classList.contains('active')) return null;`,
		`return loadServiceStatus();`,
		`setInterval(refreshServiceStatusAutomatically, 10000)`,
		`document.addEventListener('visibilitychange'`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("service status refresh guard missing %q", want)
		}
	}
	if strings.Contains(actions, `setInterval(loadServiceStatus, 10000)`) {
		t.Fatal("automatic status refresh must use the visibility-aware wrapper")
	}
}

func TestServiceRestartActionsRenderDynamicServiceEndpoints(t *testing.T) {
	runtimes := []service.ManagedRuntime{{ActionName: "veil", ManualRestart: true}, {ActionName: "caddy", ManualRestart: true}}
	actions := ServiceRestartActionsJS(runtimes)
	for _, want := range []string{
		`renderServiceRestartControls`,
		`restartable`,
		`actionName`,
		`encodeURIComponent(serviceName)`,
		`/api/services/`,
		`/restart`,
		`confirm: true`,
		`if (restarted) await loadServiceStatus()`,
		`container.textContent = ''`,
		`document.createElement('button')`,
		`button.dataset.veilRestartService = actionName`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("actions missing %q:\n%s", want, actions)
		}
	}
	if strings.Contains(actions, `container.innerHTML`) {
		t.Fatal("dynamic service names must not be inserted through innerHTML")
	}
}
