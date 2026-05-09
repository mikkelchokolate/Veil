package api

import "github.com/veil-panel/veil/internal/firewall"

type firewallRuleResponse = firewall.RuleResponse
type FirewallRuleResponseBuilder = firewall.FirewallRuleResponseBuilder

func BuildFirewallRuleResponses(settings Settings, inbounds []Inbound) []firewallRuleResponse {
	return firewall.BuildRuleResponses(settings, inbounds)
}

func NewFirewallRuleResponseBuilder() FirewallRuleResponseBuilder {
	return firewall.NewFirewallRuleResponseBuilder()
}
