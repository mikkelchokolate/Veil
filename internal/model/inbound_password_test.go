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

// TestEffectiveProtocolPasswordPreservesBytes pins the protocol-scoped
// fallback-password contract (audit #331): an explicitly set inbound password
// wins byte-for-byte, whitespace-only inbound passwords count as unset, and
// the precedence runs inbound.Password -> inbound dynamic field -> legacy
// flat inbound field -> settings dynamic field -> legacy flat settings field.
// Every consumer (server renderers, validators, client export) shares this
// resolver so they cannot diverge on trimming again.
func TestEffectiveProtocolPasswordPreservesBytes(t *testing.T) {
	tests := []struct {
		name     string
		inbound  Inbound
		settings Settings
		expected string
	}{
		{
			name:     "explicit inbound password wins byte-for-byte",
			inbound:  Inbound{Password: " padded-pass ", ProtocolFields: map[string]any{"naivePassword": "dyn"}, NaivePassword: "flat"},
			expected: " padded-pass ",
		},
		{
			name:     "whitespace-only inbound password counts as unset",
			inbound:  Inbound{Password: "  ", ProtocolFields: map[string]any{"naivePassword": " dyn "}},
			expected: "dyn",
		},
		{
			name:     "dynamic field wins over legacy flat",
			inbound:  Inbound{ProtocolFields: map[string]any{"naivePassword": "dyn"}, NaivePassword: "flat"},
			expected: "dyn",
		},
		{
			name:     "legacy flat wins over settings",
			inbound:  Inbound{NaivePassword: " flat "},
			settings: Settings{ProtocolFields: map[string]any{"naivePassword": "settings-dyn"}, NaivePassword: "settings-flat"},
			expected: " flat ",
		},
		{
			name:     "settings dynamic wins over settings flat",
			settings: Settings{ProtocolFields: map[string]any{"naivePassword": "settings-dyn"}, NaivePassword: "settings-flat"},
			expected: "settings-dyn",
		},
		{
			name:     "settings flat is the last resort",
			settings: Settings{NaivePassword: " settings-flat "},
			expected: " settings-flat ",
		},
		{
			name:     "non-string dynamic field is ignored",
			inbound:  Inbound{ProtocolFields: map[string]any{"naivePassword": 42}, NaivePassword: "flat"},
			expected: "flat",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := EffectiveProtocolPassword(tc.inbound, tc.settings, "naivePassword", tc.inbound.NaivePassword, tc.settings.NaivePassword); got != tc.expected {
				t.Fatalf("EffectiveProtocolPassword = %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestEffectiveProtocolStringPrecedence pins the dynamic-field string contract
// shared by server renderers and client export (audit #331): inbound dynamic
// field -> legacy flat inbound field -> settings dynamic field -> legacy flat
// settings field, with dynamic values trimmed and flat fields byte-for-byte.
func TestEffectiveProtocolStringPrecedence(t *testing.T) {
	settings := Settings{ProtocolFields: map[string]any{"naiveUsername": "settings-dyn"}, NaiveUsername: "settings-flat"}
	if got := EffectiveProtocolString(Inbound{ProtocolFields: map[string]any{"naiveUsername": " dyn-user "}}, settings, "naiveUsername", "inbound-flat", ""); got != "dyn-user" {
		t.Fatalf("inbound dynamic should win trimmed, got %q", got)
	}
	if got := EffectiveProtocolString(Inbound{ProtocolFields: map[string]any{"naiveUsername": "  "}}, settings, "naiveUsername", " inbound-flat ", ""); got != " inbound-flat " {
		t.Fatalf("blank dynamic should fall to flat byte-for-byte, got %q", got)
	}
	if got := EffectiveProtocolString(Inbound{}, settings, "naiveUsername", "", ""); got != "settings-dyn" {
		t.Fatalf("settings dynamic should win over settings flat, got %q", got)
	}
	if got := EffectiveProtocolString(Inbound{}, Settings{}, "naiveUsername", "", " settings-flat "); got != " settings-flat " {
		t.Fatalf("settings flat is the last resort, got %q", got)
	}
}
