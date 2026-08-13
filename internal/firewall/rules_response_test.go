package firewall

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestRuleResponsesIncludePanelAndEnabledInbounds(t *testing.T) {
	rules := BuildRuleResponses(model.Settings{PanelListen: "127.0.0.1:2096"}, []model.Inbound{{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 8443, Enabled: true}})
	if len(rules) != 2 {
		t.Fatalf("rules = %+v", rules)
	}
	if rules[0].Port != 8443 || rules[0].Protocol != "udp" || rules[1].Port != 2096 || rules[1].Protocol != "tcp" {
		t.Fatalf("rules = %+v", rules)
	}
}

// TestRuleResponsesNaiveProxyUsesEffectivePublicPort locks in audit #81/#128:
// the firewall rule for a naiveproxy inbound must open the port Caddy actually
// binds (protocolFields publicPort -> flat port -> default -> 443), not the
// flat inbound.Port.
func TestRuleResponsesNaiveProxyUsesEffectivePublicPort(t *testing.T) {
	rules := BuildRuleResponses(model.Settings{}, []model.Inbound{{
		Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 8443, Enabled: true,
		ProtocolFields: map[string]any{"publicPort": float64(9443)},
	}})
	if len(rules) != 1 {
		t.Fatalf("rules = %+v", rules)
	}
	if rules[0].Port != 9443 || rules[0].Protocol != "tcp" {
		t.Fatalf("rule = %+v, want 9443/tcp (effective public port)", rules[0])
	}
}

// TestRuleResponsesNaiveProxyDefaultsToFlatPortWhenNoPublicPort ensures the
// flat port is still used when no publicPort is configured (chain falls back).
func TestRuleResponsesNaiveProxyDefaultsToFlatPortWhenNoPublicPort(t *testing.T) {
	rules := BuildRuleResponses(model.Settings{}, []model.Inbound{{
		Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 8443, Enabled: true,
	}})
	if len(rules) != 1 || rules[0].Port != 8443 {
		t.Fatalf("rules = %+v, want single 8443/tcp rule", rules)
	}
}
