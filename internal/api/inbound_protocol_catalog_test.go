package api

import "testing"

func TestInboundProtocolCatalogKnowsSupportedProtocolsAndTransports(t *testing.T) {
	catalog := NewInboundProtocolCatalog()
	for _, tc := range []struct {
		protocol  string
		transport string
		supported bool
	}{
		{"naiveproxy", "tcp", true},
		{"naiveproxy", "udp", false},
		{"hysteria2", "udp", true},
		{"hysteria2", "tcp", false},
		{"mieru", "tcp", true},
		{"mieru", "udp", true},
		{"unknown", "tcp", false},
	} {
		if got := catalog.SupportsTransport(tc.protocol, tc.transport); got != tc.supported {
			t.Fatalf("SupportsTransport(%q,%q) = %v, want %v", tc.protocol, tc.transport, got, tc.supported)
		}
	}
}

func TestInboundProtocolCatalogOwnsDisplayNamesAndFirewallServices(t *testing.T) {
	catalog := NewInboundProtocolCatalog()
	if got := catalog.DisplayNameList(); got != "NaiveProxy, Hysteria2, and Mieru" {
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
}

func TestInboundProtocolCatalogListsPanelChoices(t *testing.T) {
	choices := NewInboundProtocolCatalog().Choices()
	want := []InboundProtocolChoice{
		{Protocol: "naiveproxy", Transports: []string{"tcp"}},
		{Protocol: "hysteria2", Transports: []string{"udp"}},
		{Protocol: "mieru", Transports: []string{"tcp", "udp"}},
	}
	if len(choices) != len(want) {
		t.Fatalf("choices = %+v", choices)
	}
	for i := range want {
		if choices[i].Protocol != want[i].Protocol || len(choices[i].Transports) != len(want[i].Transports) {
			t.Fatalf("choice[%d] = %+v, want %+v", i, choices[i], want[i])
		}
		for j := range want[i].Transports {
			if choices[i].Transports[j] != want[i].Transports[j] {
				t.Fatalf("choice[%d].Transports = %+v, want %+v", i, choices[i].Transports, want[i].Transports)
			}
		}
	}
}
