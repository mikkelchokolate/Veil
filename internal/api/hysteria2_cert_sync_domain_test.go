package api

import "testing"

// Phase-2 cert sync must cover hysteria2 inbounds whose effective domain
// resolves via settings.Domain (no per-inbound domain). Regression: before the
// fix hysteria2DomainsLocked only considered InboundDomain (per-inbound), so
// such inbounds skipped sync-caddy-cert, hysteria2 failed to load its TLS cert,
// the health check reported activating/auto-restart, and the apply rolled back.
func TestHysteria2DomainsLockedFallsBackToSettingsDomain(t *testing.T) {
	state := newManagementState(ServerInfo{Mode: "dev", Domain: "hy.example.com"})
	state.settings.Domain = "hy.example.com"
	// PanelAccess "caddy" makes the hysteria2 inbound serve a Caddy-managed cert,
	// so the settings.Domain fallback must be picked up for Phase-2 cert sync.
	state.settings.PanelAccess = "caddy"
	state.inbounds = []Inbound{
		{Name: "dshdst", Protocol: "hysteria2", Enabled: true, Port: 4236, ProtocolFields: map[string]any{}},
	}
	ctx := NewManagementApplyContext(state)
	domains := ctx.hysteria2DomainsLocked()
	if len(domains) != 1 || domains[0] != "hy.example.com" {
		t.Fatalf("expected settings.Domain fallback [hy.example.com], got %v", domains)
	}
}

// Per-inbound domain must still take precedence over settings.Domain.
func TestHysteria2DomainsLockedPrefersInboundDomain(t *testing.T) {
	state := newManagementState(ServerInfo{Mode: "dev", Domain: "hy.example.com"})
	state.settings.Domain = "hy.example.com"
	state.inbounds = []Inbound{
		{Name: "dshdst", Protocol: "hysteria2", Enabled: true, Port: 4236, ProtocolFields: map[string]any{"domain": "inbound.example.com"}},
	}
	ctx := NewManagementApplyContext(state)
	domains := ctx.hysteria2DomainsLocked()
	if len(domains) != 1 || domains[0] != "inbound.example.com" {
		t.Fatalf("expected per-inbound domain [inbound.example.com], got %v", domains)
	}
}

// A hysteria2 inbound that serves the panel cert (PanelAccess != "caddy" and no
// per-inbound domain) must NOT trigger a Caddy cert sync, even when a domain
// resolves via settings.Domain. Regression: syncing blocked apply on cert
// polling and aborted before the service restart, so auto-apply produced no
// service action (calls == []) and TestInbound*TriggersAutoApply timed out.
func TestHysteria2DomainsLockedSkipsPanelCertInbounds(t *testing.T) {
	state := newManagementState(ServerInfo{Mode: "dev", Domain: "hy.example.com"})
	state.settings.Domain = "hy.example.com"
	// PanelAccess unset (not "caddy") and no per-inbound domain -> panel cert.
	state.inbounds = []Inbound{
		{Name: "dshdst", Protocol: "hysteria2", Enabled: true, Port: 4236, ProtocolFields: map[string]any{}},
	}
	ctx := NewManagementApplyContext(state)
	if domains := ctx.hysteria2DomainsLocked(); len(domains) != 0 {
		t.Fatalf("expected no cert-sync domains for panel-cert inbound, got %v", domains)
	}
}
