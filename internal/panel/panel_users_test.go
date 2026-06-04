package panel

import (
	"strings"
	"testing"
)

func TestPanelUsersCardRendersSessionAndTokenManagement(t *testing.T) {
	card := panelUsersCardHTML()
	for _, want := range []string{
		`User Management`,
		`Active Sessions`,
		`id="btn-load-sessions"`,
		`id="sessions-table-body"`,
		`API Token Rotation`,
		`id="btn-generate-api-token"`,
		`id="btn-copy-generated-api-token"`,
		`id="token-rotation-output"`,
		`VEIL_API_TOKEN`,
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("Users card missing %q", want)
		}
	}
}

func TestPanelUsersActionsRenderSessionAndTokenManagement(t *testing.T) {
	actions := panelUsersActionsJS()
	for _, want := range []string{
		`async function loadSessions()`,
		`/api/auth/sessions`,
		`async function revokeSession(id, isCurrent)`,
		`function generateReplacementAPIToken()`,
		`crypto.getRandomValues`,
		`async function copyGeneratedAPIToken()`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Users actions missing %q", want)
		}
	}
}
