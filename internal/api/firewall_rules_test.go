package api

import "testing"

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
