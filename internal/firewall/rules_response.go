package firewall

import (
	"log"
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/bindregistry"
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
		// A loopback-only panel must not appear in the public firewall set:
		// it would punch a useless external allow and be mistaken for
		// management access when UFW is enabled (install's UFWPlan skips the
		// panel rule entirely for local access — audit #356).
		if port, err := strconv.Atoi(portStr); err == nil && !panelListenIsLoopbackOnly(settings.PanelAccess, host) {
			builder.Add(port, "tcp", "Veil panel")
		}
	}
	// ACME challenge binds planned by caddyassembly — http-01 on :80 for a
	// hysteria2-only domain, or tls-alpn-01 on :443 when no Caddy listener
	// already owns it — need matching firewall openings or issuance can never
	// complete (audit #341). When a compatible Caddy listener already owns the
	// port the plan adds no challenge bind, and that owner's rule covers it.
	if plan, _, _, err := caddyassembly.BuildFinalRenderPlan(settings, inbounds); err == nil {
		challengePorts := make([]int, 0, len(plan.ACMEChallenges))
		for key := range plan.ACMEChallenges {
			if key.Network == bindregistry.ListenTCP {
				challengePorts = append(challengePorts, key.Port)
			}
		}
		sort.Ints(challengePorts)
		for _, port := range challengePorts {
			builder.Add(port, "tcp", "Veil ACME challenge")
		}
	} else {
		// Fail-closed for openings is deliberate — never punch a port the plan
		// did not name — but surface the failure so a transient plan-build
		// error is not silently mistaken for "no challenges planned" (#341).
		log.Printf("firewall: cannot enumerate ACME challenge binds: %v", err)
	}
	return builder.Rules()
}

// panelListenIsLoopbackOnly reports whether the panel is reachable on
// loopback only: either explicitly configured local access or a loopback
// listen host. A wildcard/empty host or a public address keeps the rule.
func panelListenIsLoopbackOnly(panelAccess, host string) bool {
	if strings.EqualFold(strings.TrimSpace(panelAccess), "local") {
		return true
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
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
