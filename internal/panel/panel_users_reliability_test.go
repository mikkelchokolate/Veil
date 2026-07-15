package panel

import (
	"strings"
	"testing"
)

func TestPanelUsersReliabilityGuardsStaleLists(t *testing.T) {
	js := panelUsersReliabilityJS()
	for _, want := range []string{
		`let usersLoadGeneration = 0;`,
		`const generation = ++usersLoadGeneration;`,
		`if (generation !== usersLoadGeneration) return;`,
		`let sessionsLoadGeneration = 0;`,
		`const generation = ++sessionsLoadGeneration;`,
		`if (generation !== sessionsLoadGeneration) return;`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("users reliability missing stale-list guard %q", want)
		}
	}
}

func TestPanelUsersReliabilitySerializesMutations(t *testing.T) {
	js := panelUsersReliabilityJS()
	for _, want := range []string{
		`let userMutationInFlight = false;`,
		`async function withUserMutation(action)`,
		`if (userMutationInFlight) return null;`,
		`userMutationInFlight = true;`,
		`return await action();`,
		`userMutationInFlight = false;`,
		`setUserMutationControlsDisabled(false);`,
		`revokeSession = async function`,
		`saveUser = async function`,
		`deleteUser = async function`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("users reliability missing mutation guard %q", want)
		}
	}
}

func TestPanelUsersReliabilityValidatesFormsAndResponses(t *testing.T) {
	js := panelUsersReliabilityJS()
	for _, want := range []string{
		`form && !form.checkValidity()`,
		`form.reportValidity();`,
		`if (!Array.isArray(users))`,
		`if (!Array.isArray(sessions))`,
		`formatAPIError(text, response.status)`,
		`edit.dataset.userMutation = 'true';`,
		`remove.dataset.userMutation = 'true';`,
		`revoke.dataset.userMutation = 'true';`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("users reliability missing validation %q", want)
		}
	}
}

func TestExportedUsersActionsIncludeReliabilityLayer(t *testing.T) {
	js := UsersActionsJS()
	if !strings.Contains(js, `let userMutationInFlight = false;`) {
		t.Fatal("exported Users actions must include reliability overrides")
	}
}
