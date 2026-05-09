package firewall

import (
	"testing"

	"github.com/veil-panel/veil/internal/model"
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
