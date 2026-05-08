package api

import (
	"strings"
	"testing"
)

func TestPanelSettingsStackOptionsRenderAllSupportedStacks(t *testing.T) {
	html := panelSettingsStackOptionsHTML()
	for _, stack := range NewStackSelectionCatalog().Stacks() {
		want := `<option value="` + stack + `">` + stack + `</option>`
		if !strings.Contains(html, want) {
			t.Fatalf("stack options missing %q in %s", want, html)
		}
	}
}

func TestPanelSettingsCardIncludesMieruAndPanelStacks(t *testing.T) {
	card := panelSettingsCardHTML()
	for _, want := range []string{`<option value="panel">panel</option>`, `<option value="mieru">mieru</option>`} {
		if !strings.Contains(card, want) {
			t.Fatalf("Settings card missing stack option %q:\n%s", want, card)
		}
	}
}

func TestStackSelectionCatalogOwnsProfilePreviewDomainRequirements(t *testing.T) {
	catalog := NewStackSelectionCatalog()
	for _, stack := range []string{"panel", "mieru"} {
		if catalog.RequiresDomain(stack) {
			t.Fatalf("stack %q must not require domain/email", stack)
		}
	}
	for _, stack := range []string{"both", "naive", "hysteria2"} {
		if !catalog.RequiresDomain(stack) {
			t.Fatalf("stack %q must require domain/email", stack)
		}
	}
}
