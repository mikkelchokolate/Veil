package model

import (
	"strings"
)

// InboundDomain returns the inbound-specific domain declared in
// inbound.ProtocolFields["domain"], trimmed of whitespace and normalized to
// lowercase. It does NOT fall back to settings.Domain. An empty string is
// returned when the inbound has no valid per-inbound domain.
func InboundDomain(inbound Inbound) string {
	if inbound.ProtocolFields == nil {
		return ""
	}
	v, ok := inbound.ProtocolFields["domain"].(string)
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
// It prefers inbound.ProtocolFields["domain"] and falls back to
// settings.Domain. The result is trimmed and lowercased.
func ResolveInboundDomain(inbound Inbound, settings Settings) string {
	if d := InboundDomain(inbound); d != "" {
		return d
	}
	return strings.ToLower(strings.TrimSpace(settings.Domain))
}

// InboundEmail returns the inbound-specific ACME email declared in
// inbound.ProtocolFields["email"], trimmed of whitespace. It does NOT fall
// back to settings-level emails.
func InboundEmail(inbound Inbound) string {
	if inbound.ProtocolFields == nil {
		return ""
	}
	v, ok := inbound.ProtocolFields["email"].(string)
	if !ok {
		return ""
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	return v
}

// ResolveInboundEmail returns the effective ACME email for an inbound.
// It prefers inbound.ProtocolFields["email"] and falls back to
// settings.DefaultAcmeEmail, settings.PanelEmail, and finally settings.Email.
func ResolveInboundEmail(inbound Inbound, settings Settings) string {
	if e := InboundEmail(inbound); e != "" {
		return e
	}
	for _, e := range []string{settings.DefaultAcmeEmail, settings.PanelEmail, settings.Email} {
		if v := strings.TrimSpace(e); v != "" {
			return v
		}
	}
	return ""
}
