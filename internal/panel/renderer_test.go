package panel

import (
	"strings"
	"testing"
)

func TestRendererReplacesSlotsAndPrefixesPanelPaths(t *testing.T) {
	renderer := NewRenderer([]RenderSlot{
		{Placeholder: "__VEIL_PANEL_INTRO_CARDS__", Render: func() string { return `<button data-url="/api/version">Version</button>` }},
	})

	html := renderer.HTML("/secret/")
	if !strings.Contains(html, `<button data-url="/secret/api/version">Version</button>`) {
		t.Fatalf("Panel HTML did not render slot and prefix path:\n%s", html)
	}
	if strings.Contains(html, "__VEIL_PANEL_INTRO_CARDS__") {
		t.Fatalf("Panel HTML left slot placeholder unresolved")
	}
}
