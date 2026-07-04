package firewall

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestBuildRuleResponsesSkipsDisabledAndUnsupportedTransports(t *testing.T) {
	settings := model.Settings{PanelListen: "127.0.0.1:2096"}
	inbounds := []model.Inbound{
		{Name: "disabled", Protocol: "hysteria2", Transport: "udp", Port: 1000, Enabled: false},
		{Name: "unsupported-transport", Protocol: "hysteria2", Transport: "tcp", Port: 2000, Enabled: true},
		{Name: "valid", Protocol: "hysteria2", Transport: "udp", Port: 8443, Enabled: true},
	}
	rules := BuildRuleResponses(settings, inbounds)
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %+v", rules)
	}
	if !hasFirewallRule(rules, 8443, "udp") || !hasFirewallRule(rules, 2096, "tcp") {
		t.Fatalf("unexpected rules = %+v", rules)
	}
}

func TestFirewallRuleResponseBuilderIgnoresInvalidAndDuplicates(t *testing.T) {
	builder := NewFirewallRuleResponseBuilder()
	builder.Add(-1, "tcp", "invalid port")
	builder.Add(443, "icmp", "invalid protocol")
	builder.Add(443, "tcp", "Veil panel HTTPS")
	builder.Add(443, "tcp", "duplicate")
	builder.Add(8443, "udp", "Veil Hysteria2")
	builder.Add(8443, "udp", "duplicate")

	want := []RuleResponse{
		{Port: 443, Protocol: "tcp", Service: "Veil panel HTTPS"},
		{Port: 8443, Protocol: "udp", Service: "Veil Hysteria2"},
	}
	got := builder.Rules()
	if len(got) != len(want) {
		t.Fatalf("rules = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("rule %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestBuildRuleResponsesHandlesPanelListenErrors(t *testing.T) {
	settings := model.Settings{PanelListen: "not-a-valid-address"}
	inbounds := []model.Inbound{
		{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 8443, Enabled: true},
	}
	rules := BuildRuleResponses(settings, inbounds)
	if len(rules) != 1 || rules[0].Port != 8443 {
		t.Fatalf("expected only inbound rule, got %+v", rules)
	}
}
