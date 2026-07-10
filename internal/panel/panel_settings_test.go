package panel

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
		`settingsFieldSchema`,
		`data.protocolFields || {}`,
		`protocolFields: collectSettingsProtocolFields()`,
		`data-settings-protocol-key`,
		`field.type === 'password'`,
		`field.type === 'checkbox'`,
		`field.type === 'number'`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Settings actions missing %q", want)
		}
	}
}

func TestPanelSettingsCardModuleRendersSchemaDrivenSettingsControls(t *testing.T) {
	card := panelSettingsCardHTML()
	for _, want := range []string{
		`<h2>Settings</h2>`,
		`id="settings-form"`,
		`id="settings-panel-listen" required`,
		`id="settings-mode" required`,
		`id="settings-protocol-fields"`,
		`[REDACTED]`,
		`id="save-settings"`,
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("Settings card missing %q", want)
		}
	}
	for _, legacyID := range []string{
		`id="settings-naive-password"`,
		`id="settings-hysteria2-password"`,
		`id="settings-olcrtc-auth"`,
	} {
		if strings.Contains(card, legacyID) {
			t.Fatalf("settings card should not duplicate plugin-owned field %q", legacyID)
		}
	}
}

func TestPanelCatalogMountsSettingsCardAndActions(t *testing.T) {
	catalog := NewSliceCatalog(nil)
	renderer := NewRenderer(catalog.RenderSlots())
	html := renderer.BaseHTML()
	for _, want := range []string{
		`id="settings-card"`,
		`id="settings-form"`,
		`async function loadSettingsIntoForm()`,
		`loadSettingsIntoForm();`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered Panel is missing mounted settings module %q", want)
		}
	}
}
