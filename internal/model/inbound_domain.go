package model

import (
	"strings"
)

// Field keys used in inbound.ProtocolFields for per-inbound domain and ACME
// email configuration. Centralized here so validators, UI schemas, and client
// exporters share a single source of truth.
const (
	InboundDomainField = "domain"
	InboundEmailField  = "email"
)

// DefaultNaiveUsername is the single source of truth for the fallback naiveproxy
// username when none is configured on the inbound or in global settings. The UI
// schema, validators (apply-plan and protocol plugin), the caddy renderer, and
// the client-access exporter all share it so that a freshly created naiveproxy
// inbound (which only has a generated password) resolves a consistent, usable
// credential instead of producing an empty username / broken client link.
const DefaultNaiveUsername = "veil"

// InboundDomain returns the inbound-specific domain declared in
// inbound.ProtocolFields[InboundDomainField], trimmed of whitespace and
// normalized to lowercase. It does NOT fall back to settings.Domain. An empty
// string is returned when the inbound has no valid per-inbound domain.
func InboundDomain(inbound Inbound) string {
	if inbound.ProtocolFields == nil {
		return ""
	}
	v, ok := inbound.ProtocolFields[InboundDomainField].(string)
	if !ok {
		return ""
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	return strings.ToLower(v)
}

// ResolveInboundDomain returns the effective public domain for an inbound.
// It prefers inbound.ProtocolFields[InboundDomainField] and falls back to
// settings.Domain. The result is trimmed and lowercased.
func ResolveInboundDomain(inbound Inbound, settings Settings) string {
	if d := InboundDomain(inbound); d != "" {
		return d
	}
	return strings.ToLower(strings.TrimSpace(settings.Domain))
}

// InboundEmail returns the inbound-specific ACME email declared in
// inbound.ProtocolFields[InboundEmailField], trimmed of whitespace. It does NOT
// fall back to settings-level emails.
func InboundEmail(inbound Inbound) string {
	if inbound.ProtocolFields == nil {
		return ""
	}
	v, ok := inbound.ProtocolFields[InboundEmailField].(string)
	if !ok {
		return ""
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	return v
}

// ResolveInboundEmail returns the effective ACME email for an inbound-specific
// domain. It prefers inbound.ProtocolFields["email"] and falls back to
// settings.DefaultAcmeEmail and settings.PanelEmail. It intentionally does NOT
// fall back to the legacy settings.Email, because that global email is reserved
// for the panel's own domain and should not silently issue certificates for
// unrelated inbound domains.
func ResolveInboundEmail(inbound Inbound, settings Settings) string {
	if e := InboundEmail(inbound); e != "" {
		return e
	}
	for _, e := range []string{settings.DefaultAcmeEmail, settings.PanelEmail} {
		if v := strings.TrimSpace(e); v != "" {
			return v
		}
	}
	return ""
}
