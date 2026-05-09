package panel

import (
	"strings"
	"testing"
)

func TestPanelInboundSaveDoesNotDuplicateBackendPasswordPolicy(t *testing.T) {
	actions := panelInboundActionsJS()
	if strings.Contains(actions, "Auto-generate password for new single-profile Inbounds") || strings.Contains(actions, "genInboundPassword();\n        payload.password") {
		t.Fatalf("Panel save path should leave empty new Inbound passwords to backend policy:\n%s", actions)
	}
	if !strings.Contains(actions, "function genInboundPassword()") {
		t.Fatalf("explicit Generate button action should remain available")
	}
}
