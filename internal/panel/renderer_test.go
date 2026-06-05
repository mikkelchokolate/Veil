package panel

import (
	"strings"
	"testing"
)

func TestRendererReplacesSlotsAndPrefixesPanelPaths(t *testing.T) {
	renderer := NewRenderer([]RenderSlot{
		{Placeholder: "__VEIL_PANEL_INTRO_CARDS__", Render: func() string { return `<button data-url="/api/version">Version</button>` }},
	})

	html := renderer.HTML("/secret/", "", "ru")
	if !strings.Contains(html, `<button data-url="/secret/api/version">Version</button>`) {
		t.Fatalf("Panel HTML did not render slot and prefix path:\n%s", html)
	}
	if strings.Contains(html, "__VEIL_PANEL_INTRO_CARDS__") {
		t.Fatalf("Panel HTML left slot placeholder unresolved")
	}
}

func TestRendererIncludesLocalizedShell(t *testing.T) {
	html := NewRenderer(nil).HTML("/", "csrf-token", "ru")
	for _, want := range []string{
		`<html lang="ru">`,
		`data-veil-locale-select`,
		`window.veilLocale = "ru"`,
		`window.veilT`,
		`id="current-page-title"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("localized Panel missing %q", want)
		}
	}
}

func TestRendererIncludesMobilePanelLayout(t *testing.T) {
	html := NewRenderer(nil).BaseHTML()
	for _, want := range []string{
		"@media (max-width: 760px)",
		"body {\n        display: block;",
		".sidebar {\n        position: static;",
		".content-wrapper {\n        margin-left: 0;",
		".nav-menu {\n        flex-direction: row;",
		".table-container {\n        max-width: 100%;",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("Panel HTML missing mobile layout rule %q", want)
		}
	}
}

func TestSliceCatalogContainsUsersSlice(t *testing.T) {
	catalog := NewSliceCatalog(nil)
	slice, ok := catalog.Slice("users")
	if !ok {
		t.Fatalf("SliceCatalog missing 'users' slice")
	}
	hasCardPlaceholder := false
	hasActionsPlaceholder := false
	for _, slot := range slice.RenderSlots {
		if slot.Placeholder == UsersCardPlaceholder {
			hasCardPlaceholder = true
		}
		if slot.Placeholder == UsersActionsPlaceholder {
			hasActionsPlaceholder = true
		}
	}
	if !hasCardPlaceholder || !hasActionsPlaceholder {
		t.Fatalf("users slice missing placeholders: card=%t, actions=%t", hasCardPlaceholder, hasActionsPlaceholder)
	}
}
