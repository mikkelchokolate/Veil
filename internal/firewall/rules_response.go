package firewall

import (
	"net"
	"strconv"

	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/protocols"
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
	catalog := protocols.NewCatalog()
	for _, inbound := range inbounds {
		if !inbound.Enabled || !catalog.SupportsTransport(inbound.Protocol, inbound.Transport) {
			continue
		}
		if service, ok := catalog.FirewallService(inbound.Protocol); ok {
			builder.Add(inbound.Port, inbound.Transport, service)
		}
	}
	if settings.PanelAccess == "caddy" {
		builder.Add(443, "tcp", "Veil panel HTTPS")
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
