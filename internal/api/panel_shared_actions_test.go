package api

import (
	"strings"
	"testing"
)

func TestPanelUtilityActionsModuleRendersSharedHelpers(t *testing.T) {
	actions := panelUtilityActionsJS()
	for _, want := range []string{
		`function parseReserved(value)`,
		`function numberOrZero(id)`,
		`Number.isInteger`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Utility actions missing %q", want)
		}
	}
}
