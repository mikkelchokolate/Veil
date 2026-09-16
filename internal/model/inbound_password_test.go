package model

import "testing"

// TestEffectiveInboundPasswordPreservesBytes pins the credential-byte
// contract for the inbound-level fallback password (audit #311): the dynamic
// field wins when non-empty, and the winning value is returned byte-for-byte
// so the server config and every client export carry identical bytes.
func TestEffectiveInboundPasswordPreservesBytes(t *testing.T) {
	tests := []struct {
		name     string
		inbound  Inbound
		expected string
	}{
		{
			name:     "dynamic wins byte-for-byte",
			inbound:  Inbound{Password: "flat", ProtocolFields: map[string]any{"password": " dyn "}},
			expected: " dyn ",
		},
		{
			name:     "blank dynamic falls back to flat",
			inbound:  Inbound{Password: " flat ", ProtocolFields: map[string]any{"password": "   "}},
			expected: " flat ",
		},
		{
			name:     "absent dynamic uses flat",
			inbound:  Inbound{Password: " flat "},
			expected: " flat ",
		},
		{
			name:     "internal whitespace preserved",
			inbound:  Inbound{ProtocolFields: map[string]any{"password": "a b\tc"}},
			expected: "a b\tc",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := EffectiveInboundPassword(tc.inbound); got != tc.expected {
				t.Fatalf("EffectiveInboundPassword = %q, want %q", got, tc.expected)
			}
		})
	}
}
