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

func TestPanelDetectsLocalAnonymousAdminAccess(t *testing.T) {
	js := panelIntroReliableActionsJS()
	for _, want := range []string{
		`backend grants dev-anonymous administrator access without a static token`,
		`const response = await fetch('/api/version', {`,
		`credentials: 'same-origin',`,
		`cache: 'no-store'`,
		`return response.ok;`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("effective admin access probe missing %q", want)
		}
	}
	if strings.Contains(js, `if (!localStorage.getItem('veil_api_token')) return false;`) {
		t.Fatal("local development admin detection is still blocked by the absence of a static token")
	}
}

func TestPanelAuthStatusRefreshesSessionIdentityTokens(t *testing.T) {
	js := panelIntroReliableActionsJS()
	for _, want := range []string{
		`const refreshedCSRFToken = String(data.csrfToken || '');`,
		`window.veil_csrf_token = refreshedCSRFToken;`,
		`localStorage.setItem('veil_csrf_token', refreshedCSRFToken);`,
		`localStorage.removeItem('veil_csrf_token');`,
		`const refreshedUsername = String(data.username || '');`,
		`localStorage.setItem('veil_username', refreshedUsername);`,
		`localStorage.removeItem('veil_username');`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("authenticated status refresh missing %q", want)
		}
	}

	html := NewRenderer(NewSliceCatalog(nil).RenderSlots()).BaseHTML()
	if !strings.Contains(html, `window.veil_csrf_token = refreshedCSRFToken;`) {
		t.Fatal("rendered Panel does not synchronize refreshed CSRF tokens")
	}
}

func TestPanelLogoutPreservesLocalSessionWhenServerLogoutFails(t *testing.T) {
	js := panelIntroReliableActionsJS()
	for _, want := range []string{
		`let logoutInFlight = false;`,
		`if (logoutInFlight) return;`,
		`credentials: 'same-origin',`,
		`const text = await response.text();`,
		`if (!response.ok) {`,
		`throw new Error(formatAPIError(text, response.status));`,
		`logoutInFlight = false;`,
		`logoutBtn.disabled = false;`,
		`alert(veilT('status.requestFailed', {`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("logout reliability missing %q", want)
		}
	}
	if strings.Contains(js, `        } catch (_) {}
        invalidateCurrentUserRoleRefresh();`) {
		t.Fatal("logout still discards server failures before clearing local identity")
	}

	html := NewRenderer(NewSliceCatalog(nil).RenderSlots()).BaseHTML()
	if strings.Count(html, `let logoutInFlight = false;`) != 1 {
		t.Fatal("rendered Panel does not mount the reliable logout handler exactly once")
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
