package api

import (
	"strings"
	"testing"
)

func TestPanelClientLinksActionsModuleRendersCredentialDisclosureActions(t *testing.T) {
	actions := panelClientLinksActionsJS()
	for _, want := range []string{
		`async function loadClientLinks()`,
		`async function loadClientSubscription()`,
		`async function loadRawClientSubscription()`,
		`async function copyClientLinksOutput()`,
		`async function downloadClientSubscriptionPath(path, filename)`,
		`navigator.clipboard.writeText`,
		`URL.createObjectURL`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Client links actions missing %q", want)
		}
	}
}

func TestPanelClientLinksCardModuleRendersCredentialDisclosureControls(t *testing.T) {
	card := panelClientLinksCardHTML()
	for _, want := range []string{
		`<h2>Client links</h2>`,
		`id="load-client-links"`,
		`id="load-client-subscription"`,
		`id="load-client-subscription-raw"`,
		`id="download-client-subscription"`,
		`id="copy-client-links"`,
		`id="client-links-output"`,
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("Client links card missing %q", want)
		}
	}
}
