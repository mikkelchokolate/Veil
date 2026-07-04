package panel

import (
	"strings"
	"testing"
)

func TestRendererSkipsNilRenderSlots(t *testing.T) {
	renderer := NewRenderer([]RenderSlot{
		{Placeholder: "__VEIL_PANEL_INTRO_CARDS__", Render: func() string { return "rendered" }},
		{Placeholder: "__VEIL_PANEL_UNUSED_SLOT__", Render: nil},
	})

	html := renderer.BaseHTML()
	if !strings.Contains(html, "rendered") {
		t.Fatal("Renderer did not render the non-nil slot")
	}
	// The nil slot's placeholder should be cleaned up by the placeholder regex.
	if strings.Contains(html, "__VEIL_PANEL_UNUSED_SLOT__") {
		t.Fatalf("Renderer left nil slot placeholder in output:\n%s", html)
	}
}

func TestRendererHTMLNoBasePath(t *testing.T) {
	renderer := NewRenderer(nil)
	for _, basePath := range []string{"", "/"} {
		html := renderer.HTML(basePath, "token", LocaleEnglish)
		if !strings.Contains(html, "window.veil_csrf_token = 'token';") {
			t.Fatalf("HTML(%q) did not inject CSRF token", basePath)
		}
		if strings.Contains(html, "/secret/api/") {
			t.Fatalf("HTML(%q) should not rewrite API paths", basePath)
		}
	}
}
