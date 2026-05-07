package api

import (
	"strings"
	"testing"
)

func TestPanelSettingsActionsModuleRendersLoadAndSaveActions(t *testing.T) {
	actions := panelSettingsActionsJS()
	for _, want := range []string{
		`async function loadSettingsIntoForm()`,
		`async function saveSettings(event)`,
		`/api/settings`,
		`settings-naive-password`,
		`hysteria2Password`,
		`fallbackRoot`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Settings actions missing %q", want)
		}
	}
}

func TestPanelSettingsCardModuleRendersRedactedSettingsControls(t *testing.T) {
	card := panelSettingsCardHTML()
	for _, want := range []string{
		`<h2>Settings</h2>`,
		`id="settings-form"`,
		`id="settings-panel-listen"`,
		`id="settings-naive-password"`,
		`id="settings-hysteria2-password"`,
		`[REDACTED]`,
		`id="save-settings"`,
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("Settings card missing %q", want)
		}
	}
}
