package panel

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/service"
)

func TestServiceRestartActionsBindsRuntimeButtonsDynamically(t *testing.T) {
	runtimes := []service.ManagedRuntime{
		{ActionName: "veil", ManualRestart: true},
		{ActionName: "auto-managed", ManualRestart: false},
	}
	actions := ServiceRestartActionsJS(runtimes)
	if !strings.Contains(actions, "bindServiceRestartButton") {
		t.Fatal("actions missing dynamic restart button binder")
	}
	if !strings.Contains(actions, "data-veil-restart-service") {
		t.Fatal("actions missing restart button selector")
	}
	if strings.Contains(actions, "restart-auto-managed") {
		t.Fatalf("actions should not hard-code non-manual runtime:\n%s", actions)
	}
}
