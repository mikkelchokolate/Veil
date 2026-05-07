package api

import "testing"

func TestStackProtocolPolicyIncludesExpectedProtocols(t *testing.T) {
	cases := []struct {
		stack    string
		protocol string
		want     bool
	}{
		{"both", "naiveproxy", true},
		{"both", "hysteria2", true},
		{"naive", "naiveproxy", true},
		{"naive", "hysteria2", false},
		{"hysteria2", "hysteria2", true},
		{"hysteria2", "naiveproxy", false},
		{"bogus", "naiveproxy", false},
	}
	for _, tc := range cases {
		if got := NewStackProtocolPolicy(tc.stack).Includes(tc.protocol); got != tc.want {
			t.Fatalf("Includes(%q,%q) = %v, want %v", tc.stack, tc.protocol, got, tc.want)
		}
	}
}
