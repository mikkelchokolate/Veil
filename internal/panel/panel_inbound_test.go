package panel

import (
	"strings"
	"testing"
)

func TestPanelInboundActionsModuleRendersInboundManagementActions(t *testing.T) {
	actions := panelInboundActionsJS()
	for _, want := range []string{
		`async function loadInboundsIntoOutput()`,
		`function randomPassword()`,
		`function genInboundPassword()`,
		`async function saveInbound(event)`,
		`async function deleteInbound()`,
		`payload.profiles`,
		`/api/inbounds`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Inbound actions missing %q", want)
		}
	}
}

func TestPanelInboundFormModuleRendersInboundAndClientProfileControls(t *testing.T) {
	form := panelInboundFormHTML()
	for _, want := range []string{
		`<h2>Inbounds</h2>`,
		`id="inbound-form"`,
		`id="inbound-password"`,
		panelClientProfileControlsPlaceholder,
		`id="save-inbound"`,
		`id="delete-inbound"`,
	} {
		if !strings.Contains(form, want) {
			t.Fatalf("Inbound form missing %q", want)
		}
	}
}

func TestPanelInboundFormRendersAccessibleLiveValidation(t *testing.T) {
	form := panelInboundFormHTML()
	for _, want := range []string{
		`id="inbound-validation-summary"`,
		`role="status"`,
		`aria-live="polite"`,
		`aria-invalid="false"`,
		`aria-describedby="inbound-name-validation"`,
		`id="inbound-name-validation"`,
		`aria-describedby="inbound-port-validation"`,
		`id="inbound-port-validation"`,
	} {
		if !strings.Contains(form, want) {
			t.Fatalf("Inbound validation form missing %q", want)
		}
	}
}

func TestPanelInboundActionsDebounceAndCancelLiveValidation(t *testing.T) {
	actions := panelInboundActionsJS()
	for _, want := range []string{
		`const inboundValidationDebounceMs = 300`,
		`new AbortController()`,
		`inboundValidationSequence`,
		`function scheduleInboundValidation()`,
		`async function validateInboundCandidate()`,
		`fetch('/api/validation'`,
		`aria-invalid`,
		`issue.remediation`,
		`saveButton.disabled`,
		`form.addEventListener('input', scheduleInboundValidation)`,
		`form.addEventListener('change', scheduleInboundValidation)`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Inbound validation actions missing %q", want)
		}
	}
}

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
		`function generateAndAddProfile()`,
		`Client profile name is required`,
		`name: name`,
		`password: password`,
		`enabled: true`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Client profile actions missing %q", want)
		}
	}
}
