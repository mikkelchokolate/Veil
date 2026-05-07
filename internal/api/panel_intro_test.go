package api

import (
	"strings"
	"testing"
)

func TestPanelIntroActionsModuleRendersTokenPreviewAndVersionActions(t *testing.T) {
	actions := panelIntroActionsJS()
	for _, want := range []string{
		`veil_api_token`,
		`function authHeaders()`,
		`async function loadJSON(path, outputId, options)`,
		`profile-preview-form`,
		`/api/profiles/ru-recommended/preview`,
		`load-version`,
		`/api/version`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Intro actions missing %q", want)
		}
	}
}

func TestPanelIntroCardsModuleRendersOverviewVersionTokenAndPreview(t *testing.T) {
	cards := panelIntroCardsHTML()
	for _, want := range []string{
		`Veil Panel`,
		`<h2>Version</h2>`,
		`id="load-version"`,
		`<h2>API token</h2>`,
		`id="api-token"`,
		`<h2>Profile preview</h2>`,
		`id="profile-preview-form"`,
		`id="profile-preview-output"`,
	} {
		if !strings.Contains(cards, want) {
			t.Fatalf("Intro cards missing %q", want)
		}
	}
}
