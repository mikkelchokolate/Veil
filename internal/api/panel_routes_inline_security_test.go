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
	html := panelHTMLForCatalog("/", "", panel.LocaleEnglish, NewVisibleManagedRuntimeCatalogForState(nil))
	if strings.Contains(html, "api.qrserver.com") {
		t.Fatal("rendered Panel still references the retired third-party QR service")
	}
	// The CSP is the actual enforcement — the HTML check alone would pass if
	// the policy still whitelisted the third-party image host. Run the real
	// securer so the asserted header is the one handlePanel would emit.
	_, csp, err := secureLegacyPanelHTML(html)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(csp, "api.qrserver.com") {
		t.Fatalf("panel CSP whitelists the retired third-party QR service: %q", csp)
	}
	// The QR code is rendered locally, so no external host may appear in
	// img-src (or anywhere else) beyond 'self'/data:/blob:.
	imgSrc := cspDirective(csp, "img-src")
	if imgSrc == "" {
		t.Fatalf("panel CSP has no img-src directive: %q", csp)
	}
	for _, token := range strings.Fields(imgSrc) {
		if token != "'self'" && token != "data:" && token != "blob:" {
			t.Fatalf("img-src allows non-local source %q in CSP %q", token, csp)
		}
	}
}

// cspDirective extracts the directive body (e.g. everything after "img-src")
// from a CSP header value, without the trailing semicolon.
func cspDirective(csp, name string) string {
	for _, directive := range strings.Split(csp, ";") {
		directive = strings.TrimSpace(directive)
		if strings.HasPrefix(directive, name+" ") || directive == name {
			return strings.TrimSpace(strings.TrimPrefix(directive, name))
		}
	}
	return ""
}
