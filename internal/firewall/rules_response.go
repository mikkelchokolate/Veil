package firewall

import (
	"net"
	"strconv"

	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/protocols"
	"github.com/mikkelchokolate/Veil/internal/protocols/naiveproxy"
)

type Settings = model.Settings
type Inbound = model.Inbound

type RuleResponse struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	Service  string `json:"service"`
}

type firewallRuleResponse = RuleResponse

func BuildRuleResponses(settings model.Settings, inbounds []model.Inbound) []RuleResponse {
	builder := NewFirewallRuleResponseBuilder()
	registry := protocols.NewRegistry()
	for _, inbound := range inbounds {
		if !inbound.Enabled || !registry.SupportsTransport(inbound.Protocol, inbound.Transport) {
			continue
		}
		if service, ok := registry.FirewallService(inbound.Protocol); ok {
			// naiveproxy binds the effective public port (protocolFields
			// publicPort -> flat port -> global default -> 443), not the
			// flat inbound.Port; open exactly what Caddy listens on
			// (audit #81/#128/#147).
			port := inbound.Port
			if inbound.Protocol == "naiveproxy" {
				port = naiveproxy.NaivePublicPort(settings, inbound)
			}
			builder.Add(port, inbound.Transport, service)
		}
	}
	if settings.PanelAccess == "caddy" {
		// Match caddyassembly's effective public bind. A custom PanelPublicPort
		// must be opened instead of blindly allowing 443, otherwise the panel is
		// rendered and started on one port while UFW exposes another.
		port := settings.PanelPublicPort
		if port == 0 {
			port = 443
		}
		builder.Add(port, "tcp", "Veil panel HTTPS")
	} else if _, portStr, err := net.SplitHostPort(settings.PanelListen); err == nil {
		if port, err := strconv.Atoi(portStr); err == nil {
			builder.Add(port, "tcp", "Veil panel")
		}
	}
	return builder.Rules()
}

func BuildFirewallRuleResponses(settings model.Settings, inbounds []model.Inbound) []RuleResponse {
	return BuildRuleResponses(settings, inbounds)
}

type FirewallRuleResponseBuilder struct {
	rules []RuleResponse
	seen  map[string]bool
}

func NewFirewallRuleResponseBuilder() FirewallRuleResponseBuilder {
	return FirewallRuleResponseBuilder{seen: map[string]bool{}}
}

func (b *FirewallRuleResponseBuilder) Add(port int, protocol string, service string) {
	if port <= 0 || (protocol != "tcp" && protocol != "udp") {
		return
	}
	key := protocol + ":" + strconv.Itoa(port)
	if b.seen[key] {
		return
	}
	b.seen[key] = true
	b.rules = append(b.rules, RuleResponse{Port: port, Protocol: protocol, Service: service})
}

func (b FirewallRuleResponseBuilder) Rules() []RuleResponse {
	return append([]RuleResponse(nil), b.rules...)
}
