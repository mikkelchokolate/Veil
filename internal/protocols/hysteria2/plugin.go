package hysteria2

import (
	"strings"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// Plugin implements the Hysteria2 protocol.
type Plugin struct{}

// New creates a Hysteria2 plugin instance.
func New() *Plugin { return &Plugin{} }

func (Plugin) Protocol() string        { return "hysteria2" }
func (Plugin) DisplayName() string     { return "Hysteria2" }
func (Plugin) Transports() []string    { return []string{"udp"} }
func (Plugin) RequiresCaddy() bool     { return false }
func (Plugin) FirewallService() string { return "Veil Hysteria2" }
func (Plugin) MaxEnabled() int         { return 0 }

func (Plugin) NeedsCaddyCertSync() bool { return true }

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

func protocolBool(m map[string]any, key string, fallback bool) bool {
	if m == nil {
		return fallback
	}
	v, ok := m[key]
	if !ok {
		return fallback
	}
	b, ok := v.(bool)
	if !ok {
		return fallback
	}
	return b
}

func hysteria2Password(settings model.Settings, inbound model.Inbound) string {
	password := strings.TrimSpace(inbound.Password)
	if password == "" {
		password = protocolString(inbound.ProtocolFields, "hysteria2Password", "")
	}
	if password == "" {
		password = inbound.Hysteria2Password
	}
	if password == "" {
		password = protocolString(settings.ProtocolFields, "hysteria2Password", "")
	}
	if password == "" {
		password = settings.Hysteria2Password
	}
	return password
}

func hysteria2Insecure(settings model.Settings, inbound model.Inbound) bool {
	if inbound.Hysteria2Insecure {
		return true
	}
	if protocolBool(inbound.ProtocolFields, "hysteria2Insecure", false) {
		return true
	}
	if settings.Hysteria2Insecure {
		return true
	}
	return protocolBool(settings.ProtocolFields, "hysteria2Insecure", false)
}

func masqueradeURL(settings model.Settings, inbound model.Inbound) string {
	url := protocolString(inbound.ProtocolFields, "masqueradeURL", "")
	if url == "" {
		url = inbound.MasqueradeURL
	}
	if url == "" {
		url = protocolString(settings.ProtocolFields, "masqueradeURL", "")
	}
	if url == "" {
		url = settings.MasqueradeURL
	}
	return url
}
