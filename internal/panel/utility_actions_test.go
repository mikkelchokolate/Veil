package panel

import (
	"strings"
	"testing"
)

func TestUtilityActionsJSRendersSharedHelpers(t *testing.T) {
	actions := UtilityActionsJS()
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
