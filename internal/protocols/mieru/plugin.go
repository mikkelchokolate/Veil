package mieru

import (
	"strings"
)

// Plugin implements the Mieru protocol.
type Plugin struct{}

// New creates a Mieru plugin instance.
func New() *Plugin { return &Plugin{} }

func (Plugin) Protocol() string        { return "mieru" }
func (Plugin) DisplayName() string     { return "Mieru" }
func (Plugin) Transports() []string    { return []string{"tcp", "udp"} }
func (Plugin) RequiresCaddy() bool     { return false }
func (Plugin) FirewallService() string { return "Veil Mieru" }
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
