package naiveproxy

import (
	"strings"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// Plugin implements the naiveproxy protocol.
type Plugin struct{}

// New creates a naiveproxy plugin instance.
func New() *Plugin { return &Plugin{} }

func (Plugin) Protocol() string        { return "naiveproxy" }
func (Plugin) DisplayName() string     { return "NaiveProxy" }
func (Plugin) Transports() []string    { return []string{"tcp"} }
func (Plugin) RequiresCaddy() bool     { return true }
func (Plugin) FirewallService() string { return "Veil NaiveProxy" }
func (Plugin) MaxEnabled() int         { return 0 }

func protocolString(m map[string]any, key, fallback string) string {
	if m == nil {
		return fallback
	}
	v, ok := m[key]
	if !ok {
		return fallback
	}
	s, ok := v.(string)
	if !ok {
		return fallback
	}
	return strings.TrimSpace(s)
}

func naiveUsername(settings model.Settings, inbound model.Inbound) string {
	username := protocolString(inbound.ProtocolFields, "naiveUsername", "")
	if username == "" {
		username = inbound.NaiveUsername
	}
	if username == "" {
		username = protocolString(settings.ProtocolFields, "naiveUsername", "")
	}
	if username == "" {
		username = settings.NaiveUsername
	}
	if username == "" {
		username = model.DefaultNaiveUsername
	}
	return username
}

func naivePassword(settings model.Settings, inbound model.Inbound) string {
	password := strings.TrimSpace(inbound.Password)
	if password == "" {
		password = protocolString(inbound.ProtocolFields, "naivePassword", "")
	}
	if password == "" {
		password = inbound.NaivePassword
	}
	if password == "" {
		password = protocolString(settings.ProtocolFields, "naivePassword", "")
	}
	if password == "" {
		password = settings.NaivePassword
	}
	return password
}

// NaiveDomain returns the public domain for the inbound, preferring the inbound
// ProtocolFields and falling back to the global settings domain. The result is
// trimmed and lowercased.
func NaiveDomain(settings model.Settings, inbound model.Inbound) string {
	return model.ResolveInboundDomain(inbound, settings)
}

// NaiveEmail returns the ACME contact email explicitly set on the inbound,
// trimmed of whitespace. It does not fall back to global settings; callers that
// need the effective email should resolve the domain-level chain themselves.
func NaiveEmail(_ model.Settings, inbound model.Inbound) string {
	return model.InboundEmail(inbound)
}

// NaivePublicPort returns the public port for the inbound, falling back to the
// inbound listen port, the global default inbound public port, and finally 443.
func NaivePublicPort(settings model.Settings, inbound model.Inbound) int {
	if v, ok := inbound.ProtocolFields["publicPort"]; ok {
		if n, ok := v.(float64); ok {
			return int(n)
		}
		if n, ok := v.(int); ok {
			return n
		}
	}
	if inbound.Port != 0 {
		return inbound.Port
	}
	if settings.DefaultInboundPublicPort != 0 {
		return settings.DefaultInboundPublicPort
	}
	return 443
}

// NaiveTransport returns the transport from the inbound ProtocolFields,
// defaulting to "tcp" when unset.
func NaiveTransport(inbound model.Inbound) string {
	t := stringField(inbound.ProtocolFields, "transport")
	if t == "" {
		return "tcp"
	}
	return t
}

// NaiveFallbackRoot returns the fallback web root for the inbound, falling back
// to the inbound-level fallback root, the global fallback root, and finally the
// built-in default.
func NaiveFallbackRoot(settings model.Settings, inbound model.Inbound) string {
	root := stringField(inbound.ProtocolFields, "fallbackRoot")
	if root == "" {
		root = inbound.FallbackRoot
	}
	if root == "" {
		root = settings.FallbackRoot
	}
	if root == "" {
		root = "/var/lib/veil/www"
	}
	return root
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key].(string)
	if !ok {
		return ""
	}
	return v
}
