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

func fallbackRoot(settings model.Settings, inbound model.Inbound) string {
	root := protocolString(inbound.ProtocolFields, "fallbackRoot", "")
	if root == "" {
		root = inbound.FallbackRoot
	}
	if root == "" {
		root = protocolString(settings.ProtocolFields, "fallbackRoot", "")
	}
	if root == "" {
		root = settings.FallbackRoot
	}
	return root
}
