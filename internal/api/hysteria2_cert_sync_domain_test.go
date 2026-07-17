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
