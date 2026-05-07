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
