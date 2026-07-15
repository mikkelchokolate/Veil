package panel

import (
	"strings"
	"testing"
)

func TestUtilityActionsJSRendersSharedHelpers(t *testing.T) {
	actions := UtilityActionsJS()
	for _, want := range []string{
		`function parseReserved(value)`,
		`parts.length !== 3`,
		`byte < 0 || byte > 255`,
		`function numberOrZero(id)`,
		`input.checkValidity()`,
		`Number.isInteger(value)`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Utility actions missing %q", want)
		}
	}
	if strings.Contains(actions, `.filter((value) => Number.isInteger(value))`) {
		t.Fatal("invalid reserved bytes must be rejected, not silently dropped")
	}
}
