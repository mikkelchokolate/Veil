package model

import "testing"

func TestInboundDomain(t *testing.T) {
	cases := []struct {
		name    string
		inbound Inbound
		want    string
	}{
		{"nil fields", Inbound{}, ""},
		{"missing key", Inbound{ProtocolFields: map[string]any{}}, ""},
		{"non-string", Inbound{ProtocolFields: map[string]any{"domain": 123}}, ""},
		{"whitespace only", Inbound{ProtocolFields: map[string]any{"domain": "   "}}, ""},
		{"trimmed", Inbound{ProtocolFields: map[string]any{"domain": "  Example.COM  "}}, "example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := InboundDomain(tc.inbound); got != tc.want {
				t.Fatalf("InboundDomain() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveInboundDomain(t *testing.T) {
	cases := []struct {
		name     string
		inbound  Inbound
		settings Settings
		want     string
	}{
		{"inbound wins", Inbound{ProtocolFields: map[string]any{"domain": "inbound.example.com"}}, Settings{Domain: "global.example.com"}, "inbound.example.com"},
		{"fallback to settings", Inbound{}, Settings{Domain: "global.example.com"}, "global.example.com"},
		{"inbound without global", Inbound{ProtocolFields: map[string]any{"domain": "inbound.example.com"}}, Settings{}, "inbound.example.com"},
		{"empty inbound falls back", Inbound{ProtocolFields: map[string]any{"domain": "   "}}, Settings{Domain: "global.example.com"}, "global.example.com"},
		{"lowercases settings domain", Inbound{}, Settings{Domain: "GLOBAL.EXAMPLE.COM"}, "global.example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveInboundDomain(tc.inbound, tc.settings); got != tc.want {
				t.Fatalf("ResolveInboundDomain() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestInboundEmail(t *testing.T) {
	cases := []struct {
		name    string
		inbound Inbound
		want    string
	}{
		{"nil fields", Inbound{}, ""},
		{"missing key", Inbound{ProtocolFields: map[string]any{}}, ""},
		{"non-string", Inbound{ProtocolFields: map[string]any{"email": true}}, ""},
		{"whitespace only", Inbound{ProtocolFields: map[string]any{"email": "   "}}, ""},
		{"trimmed", Inbound{ProtocolFields: map[string]any{"email": "  a@x.com  "}}, "a@x.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := InboundEmail(tc.inbound); got != tc.want {
				t.Fatalf("InboundEmail() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveInboundEmail(t *testing.T) {
	cases := []struct {
		name     string
		inbound  Inbound
		settings Settings
		want     string
	}{
		{"inbound wins", Inbound{ProtocolFields: map[string]any{"email": "inbound@x.com"}}, Settings{Email: "global@x.com"}, "inbound@x.com"},
		{"fallback default", Inbound{}, Settings{DefaultAcmeEmail: "default@x.com"}, "default@x.com"},
		{"fallback panel", Inbound{}, Settings{PanelEmail: "panel@x.com"}, "panel@x.com"},
		{"fallback global", Inbound{}, Settings{Email: "global@x.com"}, "global@x.com"},
		{"empty inbound falls back", Inbound{ProtocolFields: map[string]any{"email": "   "}}, Settings{Email: "global@x.com"}, "global@x.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveInboundEmail(tc.inbound, tc.settings); got != tc.want {
				t.Fatalf("ResolveInboundEmail() = %q, want %q", got, tc.want)
			}
		})
	}
}
