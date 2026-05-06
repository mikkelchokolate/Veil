package api

import (
	"strings"
	"testing"
)

func TestPanelClientProfileFormModuleRendersControlsAndActions(t *testing.T) {
	controls := panelClientProfileControlsHTML()
	for _, want := range []string{
		`id="client-profile-name"`,
		`id="client-profile-username"`,
		`id="client-profile-password"`,
		`onclick="genClientProfilePassword()"`,
		`onclick="addClientProfile()"`,
	} {
		if !strings.Contains(controls, want) {
			t.Fatalf("Client profile controls missing %q", want)
		}
	}

	actions := panelClientProfileActionsJS()
	for _, want := range []string{
		`function genClientProfilePassword()`,
		`function addClientProfile()`,
		`Client profile name is required`,
		`profiles.push({ name, username: username || name, password, enabled: true })`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Client profile actions missing %q", want)
		}
	}
}

func TestPanelHTMLIncludesInboundPasswordGenerationUI(t *testing.T) {
	html := panelHTML("/secret/")
	for _, want := range []string{
		`id="inbound-password"`,
		`id="inbound-profiles"`,
		`id="client-profile-name"`,
		`id="client-profile-username"`,
		`id="client-profile-password"`,
		`addClientProfile()`,
		`genClientProfilePassword()`,
		`Client profiles`,
		`genInboundPassword()`,
		`Generate`,
		`auto-generated if empty`,
		`payload.password`,
		`payload.profiles`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("panel HTML missing %q", want)
		}
	}
}
