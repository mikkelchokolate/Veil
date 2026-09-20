package firewall

import (
	"net"
	"net/netip"
	"strconv"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/caddyassembly"
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
	} else if host, portStr, err := net.SplitHostPort(settings.PanelListen); err == nil {
		// A loopback or explicitly local panel is unreachable through the
		// firewall, so publishing a "Veil panel" allow only punches a useless
		// hole — and worse, the reconcile path can count it as management
		// access and enable UFW with no SSH allow at all (#356). Skip it the
		// same way the install-time UFWPlan skips local panels.
		if settings.PanelAccess != "local" && !isLoopbackPanelHost(host) {
			if port, err := strconv.Atoi(portStr); err == nil {
				builder.Add(port, "tcp", "Veil panel")
			}
		}
	}
	// Planned ACME challenge binds are part of the desired set, not only
	// preserved install leftovers: caddy installs leave no :80 rule, and a
	// hysteria2-only domain is switched to HTTP-01 on :80 at challenge time,
	// so apply must open the port for issuance to succeed (#341).
	if plan, _, _, err := caddyassembly.BuildFinalRenderPlan(settings, inbounds); err == nil {
		for key, owner := range plan.ACMEChallenges {
			comment := "Veil ACME HTTP-01"
			if owner.ChallengeMode == "tls-alpn-01" {
				comment = "Veil ACME TLS-ALPN-01"
			}
			builder.Add(key.Port, string(key.Network), comment)
		}
	}
	return builder.Rules()
}

// isLoopbackPanelHost reports whether a PanelListen host is loopback-only.
// Non-IP hosts (empty wildcard binds, hostnames) are conservatively treated
// as publicly reachable so the panel rule is not dropped by accident.
func isLoopbackPanelHost(host string) bool {
	host = strings.TrimSpace(host)
	if strings.EqualFold(host, "localhost") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.IsLoopback()
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
