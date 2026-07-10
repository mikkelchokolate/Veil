package panel

import (
	"strings"
	"testing"
)

func TestPanelInboundFormExposesMieruAndTransportChoices(t *testing.T) {
	html := panelInboundFormHTML()
	for _, want := range []string{
		`<option value="naiveproxy">NaiveProxy</option>`,
		`<option value="hysteria2">Hysteria2</option>`,
		`<option value="olcrtc">olcRTC</option>`,
		`<option value="mieru">Mieru</option>`,
		`<option value="tcp">tcp</option>`,
		`<option value="udp">udp</option>`,
		"NaiveProxy, Hysteria2, olcRTC, and Mieru",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("Panel Inbound form missing %q:\n%s", want, html)
		}
	}
}
