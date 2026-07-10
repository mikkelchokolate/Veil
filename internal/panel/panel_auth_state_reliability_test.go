package panel

import (
	"strings"
	"testing"
)

func TestPanelAPITokenChangesRefreshRoleGenerationSafely(t *testing.T) {
	js := panelIntroActionsJS()
	for _, want := range []string{
		`localStorage.removeItem('veil_api_token');`,
		`scheduleCurrentUserRoleRefresh();`,
		`let currentUserRoleRefreshGeneration = 0;`,
		`currentUserRoleRefreshController.abort();`,
		`signal: controller.signal`,
		`generation !== currentUserRoleRefreshGeneration`,
		`tokenSnapshot !== (localStorage.getItem('veil_api_token') || '')`,
		`error.name !== 'AbortError'`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("API token role synchronization missing %q", want)
		}
	}
}

func TestPanelLogoutAndSelfIdentityResetClearStaticToken(t *testing.T) {
	intro := panelIntroActionsJS()
	if count := strings.Count(intro, `localStorage.removeItem('veil_api_token');`); count < 2 {
		t.Fatalf("token input cleanup and logout must both remove the static token, got %d removals", count)
	}
	cleanup := panelUserIdentityCleanupJS()
	for _, want := range []string{
		`const baseClearStoredPanelIdentity = clearStoredPanelIdentity;`,
		`localStorage.removeItem('veil_api_token');`,
		`if (tokenField) tokenField.value = '';`,
	} {
		if !strings.Contains(cleanup, want) {
			t.Fatalf("user identity cleanup missing %q", want)
		}
	}

	html := NewRenderer(NewSliceCatalog(nil).RenderSlots()).BaseHTML()
	if !strings.Contains(html, `const baseClearStoredPanelIdentity = clearStoredPanelIdentity;`) {
		t.Fatal("rendered Panel does not mount static-token identity cleanup")
	}
}
