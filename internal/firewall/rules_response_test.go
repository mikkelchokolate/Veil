package firewall

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestRuleResponsesIncludePanelAndEnabledInbounds(t *testing.T) {
	// A public direct panel listener earns its UFW rule; a loopback listener
	// would not (audit #356).
	rules := BuildRuleResponses(model.Settings{PanelAccess: "direct", PanelListen: "0.0.0.0:2096"}, []model.Inbound{{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 8443, Enabled: true}})
	if len(rules) != 2 {
		t.Fatalf("rules = %+v", rules)
	}
	if rules[0].Port != 8443 || rules[0].Protocol != "udp" || rules[1].Port != 2096 || rules[1].Protocol != "tcp" {
		t.Fatalf("rules = %+v", rules)
	}
}

// #815: the apply-time firewall set must expose the panel exactly when its
// access mode makes it publicly reachable — never for local (loopback)
// access, always for direct public binds and caddy-fronted access.
func TestRuleResponsesPanelAccessLocalVsPublic(t *testing.T) {
	ruleFor := func(service string, rules []RuleResponse) *RuleResponse {
		for i := range rules {
			if rules[i].Service == service {
				return &rules[i]
			}
		}
		return nil
	}

	// local mode binds loopback — no public allow even if the listen host
	// were misconfigured public; the access mode wins.
	for _, listen := range []string{"127.0.0.1:2096", "0.0.0.0:2096"} {
		rules := BuildRuleResponses(model.Settings{PanelAccess: "local", PanelListen: listen}, nil)
		if r := ruleFor("Veil panel", rules); r != nil {
			t.Fatalf("PanelAccess=local listen=%s opened public panel rule %+v", listen, *r)
		}
	}

	// direct mode on a wildcard listener opens the configured panel port.
	rules := BuildRuleResponses(model.Settings{PanelAccess: "direct", PanelListen: "0.0.0.0:2096"}, nil)
	panel := ruleFor("Veil panel", rules)
	if panel == nil || panel.Port != 2096 || panel.Protocol != "tcp" {
		t.Fatalf("direct panel rule = %+v, want 2096/tcp Veil panel", panel)
	}
	// ...but a loopback direct listener still earns no public rule.
	rules = BuildRuleResponses(model.Settings{PanelAccess: "direct", PanelListen: "127.0.0.1:2096"}, nil)
	if r := ruleFor("Veil panel", rules); r != nil {
		t.Fatalf("loopback direct listen must not open a public rule: %+v", *r)
	}

	// caddy mode publishes on the public HTTPS port (default 443, honoring a
	// custom PanelPublicPort) regardless of the internal listen address.
	rules = BuildRuleResponses(model.Settings{PanelAccess: "caddy", PanelListen: "127.0.0.1:2096"}, nil)
	https := ruleFor("Veil panel HTTPS", rules)
	if https == nil || https.Port != 443 || https.Protocol != "tcp" {
		t.Fatalf("caddy panel rule = %+v, want 443/tcp Veil panel HTTPS", https)
	}
	rules = BuildRuleResponses(model.Settings{PanelAccess: "caddy", PanelListen: "127.0.0.1:2096", PanelPublicPort: 8443}, nil)
	https = ruleFor("Veil panel HTTPS", rules)
	if https == nil || https.Port != 8443 || https.Protocol != "tcp" {
		t.Fatalf("custom public port rule = %+v, want 8443/tcp Veil panel HTTPS", https)
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
	if len(rules) != 1 || rules[0].Port != 8443 || rules[0].Protocol != "tcp" {
		t.Fatalf("rules = %+v, want single 8443/tcp rule", rules)
	}
}
