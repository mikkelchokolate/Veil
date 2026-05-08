package api

import (
	"strings"
	"testing"
)

func TestPanelSettingsStackOptionsArePanelOnlyForLegacyClients(t *testing.T) {
	html := panelSettingsStackOptionsHTML()
	if strings.TrimSpace(html) != `<option value="panel">panel</option>` {
		t.Fatalf("legacy stack options should be panel-only, got %q", html)
	}
}

func TestPanelSettingsCardHidesStackSelection(t *testing.T) {
	card := panelSettingsCardHTML()
	for _, unwanted := range []string{`id="settings-stack"`, `>Stack</label>`, `<option value="both">both</option>`, `<option value="mieru">mieru</option>`} {
		if strings.Contains(card, unwanted) {
			t.Fatalf("Settings card should not expose stack selection %q:\n%s", unwanted, card)
		}
	}
}

func TestStackSelectionCatalogOwnsProtocolInclusion(t *testing.T) {
	catalog := NewStackSelectionCatalog()
	cases := []struct {
		stack    string
		protocol string
		want     bool
	}{
		{"panel", "naiveproxy", true},
		{"panel", "hysteria2", true},
		{"panel", "mieru", true},
		{"both", "mieru", true},
		{"naive", "hysteria2", true},
		{"panel", "unknown", false},
	}
	for _, tc := range cases {
		if got := catalog.IncludesProtocol(tc.stack, tc.protocol); got != tc.want {
			t.Fatalf("IncludesProtocol(%q,%q) = %v, want %v", tc.stack, tc.protocol, got, tc.want)
		}
	}
}

func TestStackSelectionCatalogOwnsProfilePreviewDomainRequirements(t *testing.T) {
	catalog := NewStackSelectionCatalog()
	for _, stack := range []string{"panel"} {
		if catalog.RequiresDomain(stack) {
			t.Fatalf("stack %q must not require domain/email", stack)
		}
	}
	for _, stack := range []string{"both", "naive", "hysteria2", "mieru"} {
		if catalog.RequiresDomain(stack) {
			t.Fatalf("legacy stack %q must not drive domain/email requirements", stack)
		}
	}
}
