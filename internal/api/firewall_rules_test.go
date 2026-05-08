package api

import "testing"

func TestBuildFirewallRuleResponsesIncludesMieruTransportBindings(t *testing.T) {
	rules := BuildFirewallRuleResponses(
		Settings{PanelListen: "127.0.0.1:2096"},
		[]Inbound{
			{Name: "mieru-tcp", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true},
			{Name: "mieru-udp", Protocol: "mieru", Transport: "udp", Port: 443, Enabled: true},
		},
	)
	if !hasFirewallRule(rules, 443, "tcp") || !hasFirewallRule(rules, 443, "udp") || !hasFirewallService(rules, 443, "tcp", "Veil Mieru") || !hasFirewallService(rules, 443, "udp", "Veil Mieru") {
		t.Fatalf("firewall rules = %+v", rules)
	}
}

func TestBuildFirewallRuleResponsesUsesPanelAndEnabledInboundProtocols(t *testing.T) {
	rules := BuildFirewallRuleResponses(
		Settings{PanelListen: "127.0.0.1:2096"},
		[]Inbound{
			{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true},
			{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true},
		},
	)
	if !hasFirewallRule(rules, 443, "tcp") || !hasFirewallRule(rules, 443, "udp") || !hasFirewallRule(rules, 2096, "tcp") {
		t.Fatalf("firewall rules = %+v", rules)
	}
}

func hasFirewallRule(rules []firewallRuleResponse, port int, protocol string) bool {
	for _, rule := range rules {
		if rule.Port == port && rule.Protocol == protocol {
			return true
		}
	}
	return false
}

func hasFirewallService(rules []firewallRuleResponse, port int, protocol string, service string) bool {
	for _, rule := range rules {
		if rule.Port == port && rule.Protocol == protocol && rule.Service == service {
			return true
		}
	}
	return false
}
