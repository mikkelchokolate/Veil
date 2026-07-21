package api

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/panel"
)

func TestPanelHTMLForCatalogEscapesCSRFInInlineJavaScript(t *testing.T) {
	isolateCatalogEnv(t)
	token := "csrf'</script>\\\nnext"
	html := panelHTMLForCatalog("/", token, panel.LocaleEnglish, NewVisibleManagedRuntimeCatalogForState(nil))
	escaped := panel.EscapeJavaScriptString(token)
	want := "window.veil_csrf_token = '" + escaped + "';"
	if !strings.Contains(html, want) {
		t.Fatalf("rendered Panel missing escaped CSRF assignment %q", want)
	}
	if strings.Contains(html, "window.veil_csrf_token = '"+token+"';") {
		t.Fatal("rendered Panel contains raw CSRF token inside inline JavaScript")
	}
	if strings.Contains(html, "</script>\\\nnext") {
		t.Fatal("rendered Panel exposes an unescaped script terminator from the CSRF token")
	}
}

func TestPanelCSPDoesNotAllowThirdPartyQRService(t *testing.T) {
	isolateCatalogEnv(t)
	if strings.Contains(panelHTMLForCatalog("/", "", panel.LocaleEnglish, NewVisibleManagedRuntimeCatalogForState(nil)), "api.qrserver.com") {
		t.Fatal("rendered Panel still references the retired third-party QR service")
	}
}
