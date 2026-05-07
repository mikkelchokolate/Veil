package api

import "testing"

func TestClientLinkStackPolicyAllowsExpectedProtocols(t *testing.T) {
	cases := []struct {
		stack    string
		protocol string
		want     bool
	}{
		{"naive", "naiveproxy", true},
		{"naive", "hysteria2", false},
		{"hysteria2", "hysteria2", true},
		{"hysteria2", "naiveproxy", false},
		{"both", "naiveproxy", true},
		{"both", "mieru", true},
		{"mieru", "mieru", true},
		{"mieru", "naiveproxy", false},
		{"panel", "mieru", false},
		{"", "hysteria2", true},
		{"unknown", "naiveproxy", true},
	}
	for _, tc := range cases {
		if got := NewClientLinkStackPolicy(tc.stack).Allows(tc.protocol); got != tc.want {
			t.Fatalf("Allows(%q,%q) = %v, want %v", tc.stack, tc.protocol, got, tc.want)
		}
	}
}
