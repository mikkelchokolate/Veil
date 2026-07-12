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
		`id="inbound-password" type="password" autocomplete="new-password"`,
		`id="add-inbound-btn"`,
		`id="toggle-inbounds-raw"`,
		`id="close-inbound-modal"`,
		`id="cancel-inbound-modal"`,
		`id="generate-inbound-password"`,
		panelClientProfileControlsPlaceholder,
		`id="save-inbound"`,
		`id="delete-inbound"`,
	} {
		if !strings.Contains(form, want) {
			t.Fatalf("Inbound form missing %q", want)
		}
	}
	if strings.Contains(form, `onclick=`) {
		t.Fatal("Inbound form must not use inline event handlers")
	}
}

func TestPanelInboundControlsUseBoundListeners(t *testing.T) {
	js := panelInboundControlsJS()
	for _, want := range []string{
		`document.getElementById('add-inbound-btn').addEventListener('click', openAddInboundModal);`,
		`document.getElementById('toggle-inbounds-raw').addEventListener('click', () => toggleRawView('inbounds-output'));`,
		`document.getElementById('close-inbound-modal').addEventListener('click', closeInboundModal);`,
		`document.getElementById('cancel-inbound-modal').addEventListener('click', closeInboundModal);`,
		`document.getElementById('generate-inbound-password').addEventListener('click', genInboundPassword);`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("Inbound control binding missing %q", want)
		}
	}
}

func TestPanelCatalogMountsBoundInboundControls(t *testing.T) {
	html := NewRenderer(NewSliceCatalog(nil).RenderSlots()).BaseHTML()
	if !strings.Contains(html, `document.getElementById('add-inbound-btn').addEventListener('click', openAddInboundModal);`) {
		t.Fatal("rendered Panel does not mount bound inbound controls")
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
		`veilValidationIssueText(issue)`,
		`saveButton.disabled`,
		`form.addEventListener('input', scheduleInboundValidation)`,
		`function scheduleInboundValidationForChange(event)`,
		`target.matches('input:not([type="checkbox"]):not([type="radio"]), textarea')`,
		`form.addEventListener('change', scheduleInboundValidationForChange)`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Inbound validation actions missing %q", want)
		}
	}
}

func TestViewerRoleGuardPreservesValidationDisabledState(t *testing.T) {
	actions := panelIntroActionsJS()
	for _, want := range []string{
		`el.dataset.viewerGuardWasDisabled = el.disabled ? 'true' : 'false'`,
		`el.disabled = el.dataset.viewerGuardWasDisabled === 'true'`,
		`delete el.dataset.viewerGuardWasDisabled`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Viewer role guard must preserve validation-controlled disabled state; missing %q", want)
		}
	}
}

func TestPanelClientProfileFormModuleRendersControlsAndActions(t *testing.T) {
	controls := panelClientProfileControlsHTML()
	for _, want := range []string{
		`id="client-profile-name"`,
		`id="client-profile-username"`,
		`id="client-profile-password" type="password" autocomplete="new-password"`,
		`id="generate-client-profile-password"`,
		`id="add-client-profile"`,
		`id="generate-and-add-profile"`,
	} {
		if !strings.Contains(controls, want) {
			t.Fatalf("Client profile controls missing %q", want)
		}
	}
	if strings.Contains(controls, `onclick=`) {
		t.Fatal("Client profile controls must not use inline event handlers")
	}

	actions := panelClientProfileActionsJS()
	for _, want := range []string{
		`function genClientProfilePassword()`,
		`function addClientProfile()`,
		`function generateAndAddProfile()`,
		`veilT('inbounds.profileNameRequired')`,
		`name: name`,
		`password: password`,
		`enabled: true`,
		`addEventListener('click', genClientProfilePassword)`,
		`addEventListener('click', addClientProfile)`,
		`addEventListener('click', generateAndAddProfile)`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Client profile actions missing %q", want)
		}
	}
}
