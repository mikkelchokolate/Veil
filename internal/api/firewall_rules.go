package api

import (
	"net"
	"strconv"
)

type firewallRuleResponse struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	Service  string `json:"service"`
}

func BuildFirewallRuleResponses(settings Settings, inbounds []Inbound) []firewallRuleResponse {
	builder := NewFirewallRuleResponseBuilder()
	for _, inbound := range inbounds {
		if !inbound.Enabled {
			continue
		}
		switch inbound.Protocol {
		case "naiveproxy":
			builder.Add(inbound.Port, "tcp", "Veil NaiveProxy")
		case "hysteria2":
			builder.Add(inbound.Port, "udp", "Veil Hysteria2")
		case "mieru":
			builder.Add(inbound.Port, inbound.Transport, "Veil Mieru")
		}
	}
	if _, portStr, err := net.SplitHostPort(settings.PanelListen); err == nil {
		if port, err := strconv.Atoi(portStr); err == nil {
			builder.Add(port, "tcp", "Veil panel")
		}
	}
	return builder.Rules()
}

type FirewallRuleResponseBuilder struct {
	rules []firewallRuleResponse
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
	b.rules = append(b.rules, firewallRuleResponse{Port: port, Protocol: protocol, Service: service})
}

func (b FirewallRuleResponseBuilder) Rules() []firewallRuleResponse {
	return append([]firewallRuleResponse(nil), b.rules...)
}
