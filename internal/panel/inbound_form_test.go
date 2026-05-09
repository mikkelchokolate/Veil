package panel

import (
	"strings"
	"testing"
)

func TestInboundFormRendersProtocolChoicesFromPanelPackage(t *testing.T) {
	html := InboundFormHTML()
	for _, want := range []string{"NaiveProxy, Hysteria2, and Mieru", `<option value="mieru">mieru</option>`} {
		if !strings.Contains(html, want) {
			t.Fatalf("Inbound form missing %q:\n%s", want, html)
		}
	}
}
