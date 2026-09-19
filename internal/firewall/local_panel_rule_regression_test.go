package firewall

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// TestRuleResponsesSkipLocalPanel is the #356 regression: a local/loopback
// panel must not produce a "Veil panel" UFW rule on live apply — install's
// UFWPlan already skips it, and a loopback rule would both punch a useless
// public allow and count as management access when UFW is enabled.
func TestRuleResponsesSkipLocalPanel(t *testing.T) {
	rules := BuildRuleResponses(
		model.Settings{PanelAccess: "local", PanelListen: "127.0.0.1:2096"},
		[]model.Inbound{{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true}},
	)
	for _, rule := range rules {
		if rule.Port == 2096 {
			t.Fatalf("local panel must not produce a UFW rule: %+v", rules)
		}
	}
	var found bool
	for _, rule := range rules {
		if rule.Port == 443 && rule.Protocol == "udp" {
			found = true
		}
	}
	if !found {
		t.Fatalf("inbound rule missing: %+v", rules)
	}
}

// TestRuleResponsesSkipLoopbackPanelListen pins the same guard for a
// direct-mode panel that happens to listen on loopback.
func TestRuleResponsesSkipLoopbackPanelListen(t *testing.T) {
	for _, listen := range []string{"127.0.0.1:2096", "[::1]:2096", "localhost:2096"} {
		rules := BuildRuleResponses(
			model.Settings{PanelAccess: "direct", PanelListen: listen},
			nil,
		)
		for _, rule := range rules {
			if rule.Port == 2096 {
				t.Fatalf("loopback listen %q must not produce a UFW rule: %+v", listen, rules)
			}
		}
	}
}

// TestRuleResponsesKeepPublicPanelListen pins the control case: a
// direct-mode panel on a public/wildcard address still gets its rule.
func TestRuleResponsesKeepPublicPanelListen(t *testing.T) {
	for _, listen := range []string{"0.0.0.0:2096", ":2096", "203.0.113.5:2096"} {
		rules := BuildRuleResponses(
			model.Settings{PanelAccess: "direct", PanelListen: listen},
			nil,
		)
		var found bool
		for _, rule := range rules {
			if rule.Port == 2096 && rule.Protocol == "tcp" {
				found = true
			}
		}
		if !found {
			t.Fatalf("public panel listen %q lost its UFW rule: %+v", listen, rules)
		}
	}
}
