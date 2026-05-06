package api

import (
	"net"
	"strconv"
	"strings"

	"github.com/veil-panel/veil/internal/firewall"
)

type firewallRuleResponse struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	Service  string `json:"service"`
}

func BuildFirewallRuleResponses(settings Settings, inbounds []Inbound) []firewallRuleResponse {
	sharedPort := 0
	enableTCP := false
	enableUDP := false
	for _, inbound := range inbounds {
		if !inbound.Enabled {
			continue
		}
		if inbound.Port > 0 && sharedPort == 0 {
			sharedPort = inbound.Port
		}
		switch inbound.Protocol {
		case "naiveproxy":
			enableTCP = true
		case "hysteria2":
			enableUDP = true
		}
	}
	panelPort := 0
	if _, portStr, err := net.SplitHostPort(settings.PanelListen); err == nil {
		if p, err := strconv.Atoi(portStr); err == nil {
			panelPort = p
		}
	}
	plan := firewall.UFWPlan(firewall.Config{
		SharedPort: sharedPort,
		PanelPort:  panelPort,
		EnableTCP:  enableTCP,
		EnableUDP:  enableUDP,
	})
	rules := make([]firewallRuleResponse, 0, len(plan))
	for _, r := range plan {
		if len(r.Args) < 2 {
			continue
		}
		portProto := r.Args[1]
		parts := strings.SplitN(portProto, "/", 2)
		if len(parts) != 2 {
			continue
		}
		port, _ := strconv.Atoi(parts[0])
		proto := parts[1]
		service := ""
		for i, arg := range r.Args {
			if arg == "comment" && i+1 < len(r.Args) {
				service = r.Args[i+1]
				break
			}
		}
		rules = append(rules, firewallRuleResponse{Port: port, Protocol: proto, Service: service})
	}
	return rules
}
