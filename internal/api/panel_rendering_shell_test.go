package api

import (
	"strings"
	"testing"
)

func TestPanelRenderingShellReplacesAllVeilPlaceholders(t *testing.T) {
	isolateCatalogEnv(t)
	html := panelHTMLForCatalog("/", "", "en", NewVisibleManagedRuntimeCatalog())
	if strings.Contains(html, "__VEIL_PANEL_") {
		t.Fatalf("Panel rendering shell left unresolved placeholder in HTML")
	}
	for _, want := range []string{"Veil Panel", "Client profiles", "function loadServiceStatus()", "function saveInbound"} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered Panel HTML missing %q", want)
		}
	}
}

func TestPanelHTMLIncludesInboundPasswordGenerationUI(t *testing.T) {
	isolateCatalogEnv(t)
	html := panelHTMLForCatalog("/secret/", "", "en", NewVisibleManagedRuntimeCatalog())
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
		`password`,
		`payload.password`,
		`payload.profiles`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("panel HTML missing %q", want)
		}
	}
}
