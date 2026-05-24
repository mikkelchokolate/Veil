package panel

import (
	"strings"
	"testing"
)

func TestInboundFormRendersProtocolChoicesFromPanelPackage(t *testing.T) {
	html := InboundFormHTML()
	for _, want := range []string{"NaiveProxy, Hysteria2, olcRTC, and Mieru", `<option value="olcrtc">olcrtc</option>`, `<option value="mieru">mieru</option>`} {
		if !strings.Contains(html, want) {
			t.Fatalf("Inbound form missing %q:\n%s", want, html)
		}
	}
}
