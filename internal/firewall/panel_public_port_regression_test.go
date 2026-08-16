package firewall

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestRuleResponsesPanelCaddyUsesEffectivePublicPort(t *testing.T) {
	rules := BuildRuleResponses(model.Settings{
		PanelAccess:     "caddy",
		PanelPublicPort: 9443,
	}, nil)
	if len(rules) != 1 {
		t.Fatalf("rules = %+v, want one panel rule", rules)
	}
	if rules[0].Port != 9443 || rules[0].Protocol != "tcp" || rules[0].Service != "Veil panel HTTPS" {
		t.Fatalf("panel Caddy rule = %+v, want 9443/tcp", rules[0])
	}
}

func TestRuleResponsesPanelCaddyDefaultsTo443(t *testing.T) {
	rules := BuildRuleResponses(model.Settings{PanelAccess: "caddy"}, nil)
	if len(rules) != 1 || rules[0].Port != 443 || rules[0].Protocol != "tcp" {
		t.Fatalf("default panel Caddy rules = %+v, want 443/tcp", rules)
	}
}
