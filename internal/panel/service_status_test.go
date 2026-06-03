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

func TestServiceRestartActionsRenderServiceEndpoints(t *testing.T) {
	runtimes := []service.ManagedRuntime{{ActionName: "veil", ManualRestart: true}, {ActionName: "caddy", ManualRestart: true}}
	actions := ServiceRestartActionsJS(runtimes)
	for _, want := range []string{`restart-veil`, `/api/services/veil/restart`, `restart-caddy`, `/api/services/caddy/restart`, `confirm: true`, `loadServiceStatus();`} {
		if !strings.Contains(actions, want) {
			t.Fatalf("actions missing %q:\n%s", want, actions)
		}
	}
}
