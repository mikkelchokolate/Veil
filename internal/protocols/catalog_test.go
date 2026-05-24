package protocols

import "testing"

func TestCatalogOwnsInboundProtocolChoices(t *testing.T) {
	catalog := NewCatalog()
	if got := catalog.DisplayNameList(); got != "NaiveProxy, Hysteria2, olcRTC, and Mieru" {
		t.Fatalf("DisplayNameList = %q", got)
	}
	for _, tc := range []struct{ protocol, service string }{
		{"naiveproxy", "Veil NaiveProxy"},
		{"hysteria2", "Veil Hysteria2"},
		{"mieru", "Veil Mieru"},
	} {
		service, ok := catalog.FirewallService(tc.protocol)
		if !ok || service != tc.service {
			t.Fatalf("FirewallService(%q) = %q,%v want %q,true", tc.protocol, service, ok, tc.service)
		}
	}
	choices := catalog.Choices()
	if len(choices) != 4 || choices[0].Protocol != "naiveproxy" || choices[1].Protocol != "hysteria2" || choices[2].Protocol != "olcrtc" || choices[3].Protocol != "mieru" {
		t.Fatalf("choices = %+v", choices)
	}
	for _, tc := range []struct {
		protocol  string
		transport string
		caddy     bool
	}{
		{"naiveproxy", "tcp", true},
		{"hysteria2", "udp", false},
		{"olcrtc", "udp", false},
		{"mieru", "tcp", false},
		{"mieru", "udp", false},
	} {
		if !catalog.SupportsTransport(tc.protocol, tc.transport) {
			t.Fatalf("%s/%s should be supported", tc.protocol, tc.transport)
		}
		if catalog.RequiresCaddy(tc.protocol) != tc.caddy {
			t.Fatalf("RequiresCaddy(%s) mismatch", tc.protocol)
		}
	}
	if catalog.Supports("unknown") || catalog.SupportsTransport("naiveproxy", "udp") {
		t.Fatalf("catalog accepted unsupported protocol or transport")
	}
}
