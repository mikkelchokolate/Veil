package api

import (
	"strings"
	"testing"
)

func TestPanelServiceControlsRenderFromManagedRuntimeCatalog(t *testing.T) {
	controls := panelServiceRestartControlsHTML()
	actions := panelServiceRestartControlActionsJS()
	for _, runtime := range NewManagedRuntimeCatalog().Runtimes() {
		if !runtime.ManualRestart {
			continue
		}
		buttonID := `restart-` + runtime.ActionName
		path := `/api/services/` + runtime.ActionName + `/restart`
		if !strings.Contains(controls, `id="`+buttonID+`"`) {
			t.Fatalf("controls missing %s in %s", buttonID, controls)
		}
		if !strings.Contains(actions, path) {
			t.Fatalf("actions missing %s in %s", path, actions)
		}
	}
}
