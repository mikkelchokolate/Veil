package api

import "testing"

func TestClientLinkStackPolicyIsLegacyAdapterThatAllowsPanelInboundProtocols(t *testing.T) {
	cases := []struct {
		stack    string
		protocol string
		want     bool
	}{
		{"panel", "naiveproxy", true},
		{"panel", "hysteria2", true},
		{"panel", "mieru", true},
		{"naive", "mieru", true},
		{"both", "naiveproxy", true},
		{"unknown", "hysteria2", true},
		{"panel", "unknown", false},
	}
	for _, tc := range cases {
		if got := NewClientLinkStackPolicy(tc.stack).Allows(tc.protocol); got != tc.want {
			t.Fatalf("Allows(%q,%q) = %v, want %v", tc.stack, tc.protocol, got, tc.want)
		}
	}
}
