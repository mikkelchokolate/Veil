package firewall

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestBuildRuleResponsesSkipsDisabledAndUnsupportedTransports(t *testing.T) {
	settings := model.Settings{PanelListen: "0.0.0.0:2096"}
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

// #356: a loopback/local panel is unreachable through the firewall — a
// "Veil panel" allow would be a useless hole that could also be miscounted
// as management access when deciding whether UFW may be enabled.
func TestBuildRuleResponsesSkipsLocalPanelListeners(t *testing.T) {
	for _, listen := range []string{"127.0.0.1:2096", "127.0.0.2:2096", "[::1]:2096", "localhost:2096"} {
		rules := BuildRuleResponses(model.Settings{PanelListen: listen}, nil)
		if len(rules) != 0 {
			t.Fatalf("PanelListen=%q produced rules %+v, want none", listen, rules)
		}
	}
	rules := BuildRuleResponses(model.Settings{PanelListen: "0.0.0.0:2096", PanelAccess: "local"}, nil)
	if len(rules) != 0 {
		t.Fatalf("local panel access produced rules %+v, want none", rules)
	}
	// Publicly bound panels still get their allow rule.
	rules = BuildRuleResponses(model.Settings{PanelListen: "0.0.0.0:2096"}, nil)
	if len(rules) != 1 || rules[0].Port != 2096 || rules[0].Protocol != "tcp" {
		t.Fatalf("public panel rules = %+v, want 2096/tcp", rules)
	}
}

// #341: a domain served only by hysteria2 inbounds is switched to HTTP-01 on
// :80 at challenge time (there is no Caddy TLS listener for ALPN). The
// firewall plan must open :80 or issuance can never succeed.
func TestBuildRuleResponsesAddsAcmeHTTP01ForHysteria2OnlyDomain(t *testing.T) {
	rules := BuildRuleResponses(
		model.Settings{
			AcmeChallengeMode: "tls-alpn-01",
			DefaultAcmeEmail:  "admin@example.com",
		},
		[]model.Inbound{{
			Name:      "hy2",
			Protocol:  "hysteria2",
			Transport: "udp",
			Port:      8443,
			Enabled:   true,
			ProtocolFields: map[string]any{
				"domain": "h2-only.example.com",
			},
		}},
	)
	if !hasFirewallRule(rules, 80, "tcp") {
		t.Fatalf("hysteria2-only domain must open :80 for HTTP-01, got %+v", rules)
	}
	foundHTTP01 := false
	for _, rule := range rules {
		if rule.Port == 80 && rule.Protocol == "tcp" && rule.Service == "Veil ACME HTTP-01" {
			foundHTTP01 = true
		}
	}
	if !foundHTTP01 {
		t.Fatalf("expected Veil ACME HTTP-01 service label on :80, got %+v", rules)
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
