package panel

import (
	"strings"
	"testing"
)

// #577: the JS-written veil_locale cookie must be scoped to the same Path the
// server applies to veil_session (panelCookieAttrs). Under a mounted web base
// path a Path=/ preference cookie splits the cookie jar — the panel and the
// API then disagree about the user's locale.
func TestLoginHTMLScopesLocaleCookieToBasePath(t *testing.T) {
	html := LoginHTML("/panel-secret/", "en")
	if !strings.Contains(html, `'; Path=/panel-secret/; Max-Age=31536000; SameSite=Lax'`) {
		t.Fatal("login page veil_locale write is not scoped to the base path")
	}
	if strings.Contains(html, `'; Path=/;`) {
		t.Fatal("login page still writes veil_locale with Path=/")
	}
}

func TestReliableLoginHTMLScopesLocaleCookieToBasePath(t *testing.T) {
	html := ReliableLoginHTML("/panel-secret/", "en")
	if !strings.Contains(html, `'; Path=/panel-secret/; Max-Age=31536000; SameSite=Lax'`) {
		t.Fatal("reliable login veil_locale write is not scoped to the base path")
	}
	if strings.Contains(html, `'; Path=/;`) {
		t.Fatal("reliable login still writes veil_locale with Path=/")
	}
	// The reliability rewrite must still have applied under a mounted path.
	if !strings.Contains(html, `catch (cookieError)`) {
		t.Fatal("reliable login lost its best-effort locale cookie guard")
	}
}

func TestRenderedPanelScopesLocaleCookieToBasePath(t *testing.T) {
	html := NewRenderer(NewSliceCatalog(nil).RenderSlots()).HTML("/panel-secret/", "", "en")
	if !strings.Contains(html, `'; Path=/panel-secret/; Max-Age=31536000; SameSite=Lax'`) {
		t.Fatal("panel veil_locale write is not scoped to the base path")
	}
	if strings.Contains(html, `'; Path=/;`) {
		t.Fatal("panel still writes veil_locale with Path=/")
	}
}

func TestLocaleCookieStaysRootedAtRootMount(t *testing.T) {
	for _, html := range []string{
		LoginHTML("/", "en"),
		ReliableLoginHTML("/", "en"),
		NewRenderer(NewSliceCatalog(nil).RenderSlots()).HTML("/", "", "en"),
	} {
		if !strings.Contains(html, `'; Path=/; Max-Age=31536000; SameSite=Lax'`) {
			t.Fatal("root mount must keep Path=/ on the veil_locale cookie")
		}
	}
}
