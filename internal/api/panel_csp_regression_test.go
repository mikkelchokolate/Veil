package api

import (
	"regexp"
	"strings"
	"testing"
)

func TestLegacyFallbackCSPUsesNonceAndNoRemoteFonts(t *testing.T) {
	// The fixture carries BOTH Google Fonts hosts: fonts.googleapis.com serves
	// the stylesheet link and fonts.gstatic.com serves the font files. Only
	// checking one would leave the other path untested (issue #881).
	body := `<html><head><link rel="stylesheet" href="https://fonts.googleapis.com/css2"><link rel="preconnect" href="https://fonts.gstatic.com" crossorigin><style>body{color:red}</style></head><body><script>window.ok=true</script></body></html>`
	secured, csp, err := secureLegacyPanelHTML(body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(csp, "unsafe-inline") {
		t.Fatalf("fallback CSP retains unsafe-inline: %q", csp)
	}
	for _, host := range []string{"fonts.googleapis.com", "fonts.gstatic.com"} {
		if strings.Contains(csp, host) {
			t.Fatalf("fallback CSP whitelists remote font host %s: %q", host, csp)
		}
		if strings.Contains(secured, host) {
			t.Fatalf("secured HTML still references %s: %q", host, secured)
		}
	}
	noncePattern := regexp.MustCompile(`nonce="([A-Za-z0-9_-]+)"`)
	matches := noncePattern.FindAllStringSubmatch(secured, -1)
	if len(matches) != 2 || matches[0][1] != matches[1][1] || !strings.Contains(csp, "'nonce-"+matches[0][1]+"'") {
		t.Fatalf("inline tags and CSP do not share one nonce: csp=%q html=%q", csp, secured)
	}
}
