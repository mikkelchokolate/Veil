package api

import "testing"

func TestStackProtocolPolicyIsLegacyAdapterThatDoesNotSelectProtocols(t *testing.T) {
	cases := []struct {
		stack    string
		protocol string
		want     bool
	}{
		{"panel", "naiveproxy", true},
		{"panel", "hysteria2", true},
		{"panel", "mieru", true},
		{"both", "naiveproxy", true},
		{"naive", "hysteria2", true},
		{"bogus", "mieru", true},
		{"panel", "unknown", false},
	}
	for _, tc := range cases {
		if got := NewStackProtocolPolicy(tc.stack).Includes(tc.protocol); got != tc.want {
			t.Fatalf("Includes(%q,%q) = %v, want %v", tc.stack, tc.protocol, got, tc.want)
		}
	}
}
