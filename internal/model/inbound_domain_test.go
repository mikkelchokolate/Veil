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
		{"no fallback to legacy global email", Inbound{}, Settings{Email: "global@x.com"}, ""},
		{"empty inbound falls back to default", Inbound{ProtocolFields: map[string]any{"email": "   "}}, Settings{DefaultAcmeEmail: "default@x.com"}, "default@x.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveInboundEmail(tc.inbound, tc.settings); got != tc.want {
				t.Fatalf("ResolveInboundEmail() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestResolveInboundEmailInheritsLegacyEmailForPanelDomain covers audit #305:
// an enabled inbound reusing the Panel's own hostname under caddy panel access
// shares the installed certificate/account policy — including the legacy
// settings.Email — and must not demand the email be re-entered.
func TestResolveInboundEmailInheritsLegacyEmailForPanelDomain(t *testing.T) {
	settings := Settings{
		PanelAccess: "caddy",
		Domain:      "panel.example.com",
		Email:       "admin@example.com",
	}
	inbound := Inbound{
		Protocol:       "hysteria2",
		Enabled:        true,
		ProtocolFields: map[string]any{"domain": " Panel.Example.Com "},
	}
	if got := ResolveInboundEmail(inbound, settings); got != "admin@example.com" {
		t.Fatalf("panel-domain email = %q, want legacy settings.Email", got)
	}
}

// TestResolveInboundEmailStillRejectsUnrelatedDomain keeps the legacy-email
// exclusion for non-Panel domains (audit #305).
func TestResolveInboundEmailStillRejectsUnrelatedDomain(t *testing.T) {
	settings := Settings{
		PanelAccess: "caddy",
		Domain:      "panel.example.com",
		Email:       "admin@example.com",
	}
	inbound := Inbound{
		Protocol:       "hysteria2",
		Enabled:        true,
		ProtocolFields: map[string]any{"domain": "other.example.com"},
	}
	if got := ResolveInboundEmail(inbound, settings); got != "" {
		t.Fatalf("unrelated domain resolved legacy email %q", got)
	}
}

// TestResolveInboundEmailPanelDomainWithoutCaddyAccess needs an explicit or
// default email: without caddy panel access the domain is not Panel-owned and
// the legacy email must not leak (audit #305).
func TestResolveInboundEmailPanelDomainWithoutCaddyAccess(t *testing.T) {
	settings := Settings{
		PanelAccess: "direct",
		Domain:      "panel.example.com",
		Email:       "admin@example.com",
	}
	inbound := Inbound{
		Protocol:       "hysteria2",
		Enabled:        true,
		ProtocolFields: map[string]any{"domain": "panel.example.com"},
	}
	if got := ResolveInboundEmail(inbound, settings); got != "" {
		t.Fatalf("direct panel access resolved legacy email %q", got)
	}
}

// TestResolveInboundEmailPanelDomainVariant checks the PanelDomain field wins
// over Domain when resolving the panel hostname (audit #305).
func TestResolveInboundEmailPanelDomainVariant(t *testing.T) {
	settings := Settings{
		PanelAccess: "caddy",
		PanelDomain: "panel.example.com",
		Domain:      "fallback.example.com",
		Email:       "admin@example.com",
	}
	inbound := Inbound{
		Protocol:       "naiveproxy",
		Enabled:        true,
		ProtocolFields: map[string]any{"domain": "panel.example.com"},
	}
	if got := ResolveInboundEmail(inbound, settings); got != "admin@example.com" {
		t.Fatalf("PanelDomain match email = %q, want legacy settings.Email", got)
	}
}
