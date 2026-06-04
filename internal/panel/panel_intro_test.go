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
