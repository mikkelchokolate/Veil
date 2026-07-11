package panel

import (
	"strings"
	"testing"
)

func TestUserMutationsInvalidateUserAndSessionLoads(t *testing.T) {
	actions := UsersActionsJS()
	for _, want := range []string{
		`function invalidateUserResourceLoads()`,
		`usersLoadGeneration += 1;`,
		`sessionsLoadGeneration += 1;`,
		`const loadSessionsButton = document.getElementById('btn-load-sessions');`,
		`loadSessionsButton.disabled = Boolean(disabled) || isViewerRole();`,
		`if (userMutationInFlight) return null;`,
		`invalidateUserResourceLoads();`,
		`return baseWithUserMutationForResources(action);`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("user resource reliability actions missing %q", want)
		}
	}
}

func TestUserResourceReliabilityMountedOnce(t *testing.T) {
	html := NewRenderer(NewSliceCatalog(nil).RenderSlots()).BaseHTML()
	if got := strings.Count(html, `function invalidateUserResourceLoads()`); got != 1 {
		t.Fatalf("user resource reliability mounted %d times, want 1", got)
	}
}
