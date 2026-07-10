package panel

import (
	"strings"
	"testing"
)

func TestPanelIntroActionsModuleRendersTokenPreviewAndVersionActions(t *testing.T) {
	actions := panelIntroActionsJS()
	for _, want := range []string{
		`veil_api_token`,
		`function authHeaders()`,
		`function formatAPIError(text, status)`,
		`data.error.message`,
		`function currentUserRole()`,
		`function applyViewerRoleGuard()`,
		`adminOnlyControlIds`,
		`/api/auth/status`,
		`async function loadJSON(path, outputId, options)`,
		`profile-preview-form`,
		`/api/profiles/ru-recommended/preview`,
		`profile-panel-access`,
		`panelAccess === 'caddy'`,
		`load-version`,
		`/api/version`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Intro actions missing %q", want)
		}
	}
	if strings.Contains(actions, `profilePreviewDomainRequired`) || strings.Contains(actions, `profile-stack`) {
		t.Fatalf("Panel preview must not expose stack selection:\n%s", actions)
	}
}

func TestPanelRoleResolutionFailsClosedAndValidatesStaticToken(t *testing.T) {
	actions := panelIntroActionsJS()
	for _, want := range []string{
		`return currentUserRole() !== 'admin';`,
		`async function staticTokenHasAdminAccess()`,
		`const response = await fetch('/api/version', { headers: authHeaders() });`,
		`setCurrentUserRole('');`,
		`} else if (await staticTokenHasAdminAccess()) {`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("role resolution missing fail-closed behavior %q", want)
		}
	}
	if strings.Contains(actions, `else if (localStorage.getItem('veil_api_token'))`) {
		t.Fatal("an unvalidated local API token must not grant admin UI access")
	}
}

func TestPanelVersionPollingIsSequentialAndCannotOverwriteSuccessAtTimeout(t *testing.T) {
	actions := panelIntroActionsJS()
	for _, want := range []string{
		`for (let attempt = 1; attempt <= maxAttempts; attempt += 1)`,
		`await new Promise((resolve) => setTimeout(resolve, 2000));`,
		`output.textContent = veilT('version.backOnline'`,
		`return;`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("version polling missing %q", want)
		}
	}
	if strings.Contains(actions, `const pollInterval = setInterval`) {
		t.Fatal("version polling must not overlap requests through setInterval")
	}
}

func TestPanelLoadJSONTreatsNoContentAsSuccess(t *testing.T) {
	actions := panelIntroActionsJS()
	for _, want := range []string{
		`const requestOptions = Object.assign({}, options || {});`,
		`if (!text) {`,
		`return { success: true };`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("loadJSON success contract missing %q", want)
		}
	}
	if strings.Contains(actions, `const parsed = text ? JSON.parse(text) : null;`) {
		t.Fatal("loadJSON must not return null for a successful empty response")
	}
}

func TestPanelIntroCardsModuleRendersOverviewVersionTokenAndPreview(t *testing.T) {
	cards := panelIntroCardsHTML()
	for _, want := range []string{
		`Veil Panel`,
		`NaiveProxy, Hysteria2, olcRTC, and Mieru management`,
		`<h2>Version</h2>`,
		`id="load-version"`,
		`<h2>API token</h2>`,
		`id="api-token"`,
		`<h2>Panel install preview</h2>`,
		`id="profile-preview-form"`,
		`id="profile-panel-access"`,
		`id="profile-preview-output"`,
	} {
		if !strings.Contains(cards, want) {
			t.Fatalf("Intro cards missing %q", want)
		}
	}
}

func TestPanelIntroProfilePreviewDoesNotExposeStackOptions(t *testing.T) {
	cards := panelIntroCardsHTML()
	for _, unwanted := range []string{`id="profile-stack"`, `<option value="both">`, `<option value="mieru">`, `<option value="naive">`, `<option value="hysteria2">`} {
		if strings.Contains(cards, unwanted) {
			t.Fatalf("Profile preview should not expose stack option %q:\n%s", unwanted, cards)
		}
	}
}
