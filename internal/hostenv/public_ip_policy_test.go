package hostenv

import (
	"net"
	"testing"
)

func TestPublicIPPolicyAcceptsOnlyPublicAddresses(t *testing.T) {
	policy := NewPublicIPPolicy()
	cases := map[string]bool{
		"8.8.8.8":       true,
		"10.0.0.1":      false,
		"100.64.0.1":    false,
		"192.0.2.1":     false,
		"198.51.100.42": false,
		"203.0.113.10":  false,
	}
	for value, want := range cases {
		if got := policy.IsPublic(net.ParseIP(value)); got != want {
			t.Fatalf("IsPublic(%s) = %v, want %v", value, got, want)
		}
	}
}
