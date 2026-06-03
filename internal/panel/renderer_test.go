package panel

import (
	"strings"
	"testing"
)

func TestRendererReplacesSlotsAndPrefixesPanelPaths(t *testing.T) {
	renderer := NewRenderer([]RenderSlot{
		{Placeholder: "__VEIL_PANEL_INTRO_CARDS__", Render: func() string { return `<button data-url="/api/version">Version</button>` }},
	})

	html := renderer.HTML("/secret/", "")
	if !strings.Contains(html, `<button data-url="/secret/api/version">Version</button>`) {
		t.Fatalf("Panel HTML did not render slot and prefix path:\n%s", html)
	}
	if strings.Contains(html, "__VEIL_PANEL_INTRO_CARDS__") {
		t.Fatalf("Panel HTML left slot placeholder unresolved")
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
