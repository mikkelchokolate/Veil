package panel

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/service"
)

func TestServiceStatusAndRestartWorkflowsShareSingleFlightGuard(t *testing.T) {
	statusActions := ServiceStatusActionsJS()
	for _, want := range []string{
		`let serviceRestartInFlight = false;`,
		`function setServiceStatusControlsDisabled(disabled)`,
		`document.querySelectorAll('[data-veil-restart-service]')`,
		`if (serviceStatusLoadInFlight || serviceRestartInFlight) return null;`,
		`async function fetchAndRenderServiceStatus()`,
		`setServiceStatusControlsDisabled(true);`,
		`setServiceStatusControlsDisabled(serviceRestartInFlight);`,
	} {
		if !strings.Contains(statusActions, want) {
			t.Fatalf("service status actions missing %q", want)
		}
	}

	restartActions := ServiceRestartActionsJS([]service.ManagedRuntime{{ActionName: "veil", ManualRestart: true}})
	for _, want := range []string{
		`if (serviceRestartInFlight || serviceStatusLoadInFlight) return;`,
		`serviceRestartInFlight = true;`,
		`setServiceStatusControlsDisabled(true);`,
		`if (restarted) await fetchAndRenderServiceStatus();`,
		`serviceRestartInFlight = false;`,
		`setServiceStatusControlsDisabled(false);`,
		`setServiceStatusControlsDisabled(serviceStatusLoadInFlight || serviceRestartInFlight);`,
	} {
		if !strings.Contains(restartActions, want) {
			t.Fatalf("service restart actions missing %q", want)
		}
	}
	if strings.Contains(restartActions, `button.disabled = true;`) {
		t.Fatal("restart workflow must disable all managed service controls, not only the clicked button")
	}
}

func TestServiceRestartControlMarkupRemainsValid(t *testing.T) {
	html := ServiceRestartControlsHTML([]service.ManagedRuntime{{ActionName: "veil", ManualRestart: true}})
	if !strings.Contains(html, `id="restart-veil"`) || !strings.Contains(html, `data-veil-restart-service="veil"`) {
		t.Fatalf("unexpected restart control markup: %s", html)
	}
	if strings.Contains(html, `\"`) {
		t.Fatalf("restart control markup contains escaped quote literals: %s", html)
	}
}
