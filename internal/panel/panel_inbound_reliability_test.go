package panel

import (
	"strings"
	"testing"
)

func TestPanelInboundReliabilityKeepsAddAndEditPersistenceModesSeparate(t *testing.T) {
	js := panelInboundReliabilityJS()
	for _, want := range []string{
		`let veilEditingInboundName = ''`,
		`window.openAddInboundModal = function()`,
		`window.openEditInboundModal = function(name)`,
		`window.cachedInbounds.some((inbound) => inbound.name === name)`,
		`method: editingName ? 'PUT' : 'POST'`,
		`'/api/inbounds/' + encodeURIComponent(editingName)`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("inbound reliability JS missing %q", want)
		}
	}
}

func TestPanelInboundReliabilityValidatesDisabledDraftsAndRetriesSchemas(t *testing.T) {
	form := panelInboundFormHTML()
	for _, want := range []string{
		`id="inbound-name" required pattern="[A-Za-z0-9_-]+"`,
		`id="inbound-port" type="number" required min="1" max="65535"`,
	} {
		if !strings.Contains(form, want) {
			t.Fatalf("inbound form missing browser constraint %q", want)
		}
	}

	js := panelInboundReliabilityJS()
	for _, want := range []string{
		`!form.checkValidity()`,
		`form.reportValidity()`,
		`window.protocolSchemaPromise = null`,
		`Could not load protocol fields`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("inbound reliability JS missing %q", want)
		}
	}
}

func TestPanelInboundReliabilitySerializesAllMutations(t *testing.T) {
	js := panelInboundReliabilityJS()
	for _, want := range []string{
		`let inboundMutationInFlight = false;`,
		`async function withInboundMutation(action)`,
		`if (inboundMutationInFlight) return null;`,
		`setInboundMutationControlsDisabled(true);`,
		`saveInbound = async function(event)`,
		`deleteInbound = async function()`,
		`window.toggleInboundActive = async function(name, checked, control)`,
		`window.directDeleteInbound = async function(name)`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("inbound mutation serialization missing %q", want)
		}
	}
}

func TestPanelInboundReliabilityCancelsStaleLoads(t *testing.T) {
	js := panelInboundReliabilityJS()
	for _, want := range []string{
		`let inboundLoadGeneration = 0;`,
		`let inboundLoadController = null;`,
		`inboundLoadController.abort();`,
		`if (inboundMutationInFlight) return null;`,
		`signal: controller.signal`,
		`generation !== inboundLoadGeneration || controller.signal.aborted`,
		`error.name === 'AbortError'`,
		`await loadInboundsIntoOutput();`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("inbound stale-load protection missing %q", want)
		}
	}
}

func TestPanelClientProfileActionsPreserveInvalidJSONAndRevalidate(t *testing.T) {
	js := panelClientProfileActionsJS()
	for _, want := range []string{
		`function readClientProfiles()`,
		`if (!Array.isArray(profiles))`,
		`showClientProfileError(veilT('inbounds.profilesInvalid'`,
		`writeClientProfiles(profiles)`,
		`scheduleInboundValidation()`,
		`A client profile with this name already exists.`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("client profile actions missing %q", want)
		}
	}
	if strings.Contains(js, `// ignore`) {
		t.Fatal("client profile generation must not discard invalid JSON")
	}
}

func TestPanelRequestReliabilityDistinguishesEmptySuccessFromFailure(t *testing.T) {
	js := panelRequestReliabilityJS()
	for _, want := range []string{
		`if (!response.ok)`,
		`return null`,
		`if (!text)`,
		`return true`,
		`veilT('common.ok')`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("request reliability JS missing %q", want)
		}
	}
}
