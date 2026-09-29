package caddyassembly

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// Issue #1181: a direct-mode panel with a hysteria2-only domain forces the
// -acme challenge server onto :80 — so renewal can never bind :80 itself and
// must run acme.sh through the Caddy-fronted internal port instead.
func TestPanelIPCertCaddyFrontedWithHysteria2Domain(t *testing.T) {
	settings := model.Settings{
		PanelAccess:       "direct",
		DefaultAcmeEmail:  "ops@example.net",
		AcmeChallengeMode: "tls-alpn-01",
	}
	inbounds := []model.Inbound{
		{Name: "hy1", Protocol: "hysteria2", Enabled: true, Port: 443,
			ProtocolFields: map[string]any{"domain": "hy.example.net"}},
	}
	if !PanelIPCertCaddyFronted(settings, inbounds) {
		t.Fatal("hysteria2-domain plan owns :80 via -acme — renewal must go through Caddy")
	}
}

// A direct panel without any Caddy :80 listener keeps plain standalone —
// acme.sh binds the public port itself.
func TestPanelIPCertCaddyFrontedNoCaddyPort80(t *testing.T) {
	settings := model.Settings{PanelAccess: "direct", DefaultAcmeEmail: "ops@example.net"}
	if PanelIPCertCaddyFronted(settings, nil) {
		t.Fatal("no rendered :80 listener — standalone must stay on the public port")
	}
	// caddy-mode panels never take the standalone path anyway.
	settings.PanelAccess = "caddy"
	settings.Domain = "panel.example.net"
	inbounds := []model.Inbound{
		{Name: "hy1", Protocol: "hysteria2", Enabled: true, Port: 443,
			ProtocolFields: map[string]any{"domain": "hy.example.net"}},
	}
	if PanelIPCertCaddyFronted(settings, inbounds) {
		t.Fatal("caddy-mode panel never issues the standalone IP certificate")
	}
}
