package protocols

import "testing"

func TestCatalogOwnsInboundProtocolChoices(t *testing.T) {
	catalog := NewCatalog()
	if got := catalog.DisplayNameList(); got != "NaiveProxy, Hysteria2, and Mieru" {
		t.Fatalf("DisplayNameList = %q", got)
	}
	for _, tc := range []struct {
		protocol  string
		transport string
		caddy     bool
	}{
		{"naiveproxy", "tcp", true},
		{"hysteria2", "udp", false},
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
