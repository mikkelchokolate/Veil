package firewall

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// TestRuleResponsesOpenHTTP01ForHysteria2OnlyDomain is the #341 regression:
// a domain owned exclusively by hysteria2 inbounds must use http-01 (there is
// no Caddy TLS listener to answer tls-alpn-01 for it), but the desired
// firewall set never opened :80, so live apply rendered a Caddy config that
// could never complete issuance.
func TestRuleResponsesOpenHTTP01ForHysteria2OnlyDomain(t *testing.T) {
	rules := BuildRuleResponses(
		model.Settings{AcmeChallengeMode: "tls-alpn-01", DefaultAcmeEmail: "admin@example.com"},
		[]model.Inbound{{
			Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 8443, Enabled: true,
			ProtocolFields: map[string]any{"domain": "hy2.example.com"},
		}},
	)
	var http01 *RuleResponse
	for i := range rules {
		if rules[i].Port == 80 && rules[i].Protocol == "tcp" {
			http01 = &rules[i]
		}
	}
	if http01 == nil {
		t.Fatalf("rules = %+v, want a 80/tcp ACME http-01 opening", rules)
	}
	if http01.Service == "" {
		t.Fatalf("ACME rule must carry a service label: %+v", http01)
	}
}

// TestRuleResponsesNoChallengeRuleWithoutDomains pins the negative case: with
// no ACME-managed domain the challenge bind set is empty and no extra ports
// appear.
func TestRuleResponsesNoChallengeRuleWithoutDomains(t *testing.T) {
	rules := BuildRuleResponses(
		model.Settings{AcmeChallengeMode: "tls-alpn-01"},
		[]model.Inbound{{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 8443, Enabled: true}},
	)
	for _, rule := range rules {
		if rule.Port == 80 {
			t.Fatalf("unexpected :80 rule without an ACME domain: %+v", rules)
		}
	}
}
