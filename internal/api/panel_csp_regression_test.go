package api

import (
	"regexp"
	"strings"
	"testing"
)

func TestLegacyFallbackCSPUsesNonceAndNoRemoteFonts(t *testing.T) {
	body := `<html><head><link href="https://fonts.googleapis.com/css2"><style>body{color:red}</style></head><body><script>window.ok=true</script></body></html>`
	secured, csp, err := secureLegacyPanelHTML(body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(csp, "unsafe-inline") || strings.Contains(csp, "fonts.google") || strings.Contains(secured, "fonts.googleapis.com") {
		t.Fatalf("fallback CSP or HTML retains unsafe/external content: csp=%q html=%q", csp, secured)
	}
	noncePattern := regexp.MustCompile(`nonce="([A-Za-z0-9_-]+)"`)
	matches := noncePattern.FindAllStringSubmatch(secured, -1)
	if len(matches) != 2 || matches[0][1] != matches[1][1] || !strings.Contains(csp, "'nonce-"+matches[0][1]+"'") {
		t.Fatalf("inline tags and CSP do not share one nonce: csp=%q html=%q", csp, secured)
	}
}
