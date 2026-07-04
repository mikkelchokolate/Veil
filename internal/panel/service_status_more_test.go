package panel

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/service"
)

func TestServiceRestartActionsSkipsNonManualRuntimes(t *testing.T) {
	runtimes := []service.ManagedRuntime{
		{ActionName: "veil", ManualRestart: true},
		{ActionName: "auto-managed", ManualRestart: false},
	}
	actions := ServiceRestartActionsJS(runtimes)
	if !strings.Contains(actions, "restart-veil") {
		t.Fatal("actions missing manual veil restart handler")
	}
	if strings.Contains(actions, "restart-auto-managed") {
		t.Fatalf("actions should not include non-manual runtime:\n%s", actions)
	}
}
