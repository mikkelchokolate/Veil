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

func TestRuleResponsesUsePanelPublicPortForCaddyAccess(t *testing.T) {
	rules := BuildRuleResponses(model.Settings{PanelAccess: "caddy", PanelPublicPort: 8443}, nil)
	found := false
	for _, r := range rules {
		if r.Port == 8443 && r.Protocol == "tcp" && r.Service == "Veil panel HTTPS" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected panel HTTPS rule on public port 8443, got %+v", rules)
	}
}

func TestRuleResponsesOpenHTTP01ChallengePort(t *testing.T) {
	rules := BuildRuleResponses(model.Settings{AcmeChallengeMode: "http-01"}, nil)
	found := false
	for _, r := range rules {
		if r.Port == 80 && r.Protocol == "tcp" && r.Service == "Veil ACME HTTP-01" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected HTTP-01 challenge rule on port 80, got %+v", rules)
	}
}

func TestRuleResponsesOpenTLSALPN01ChallengePort(t *testing.T) {
	rules := BuildRuleResponses(model.Settings{AcmeChallengeMode: "tls-alpn-01"}, nil)
	found := false
	for _, r := range rules {
		if r.Port == 443 && r.Protocol == "tcp" && r.Service == "Veil ACME TLS-ALPN-01" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected TLS-ALPN-01 challenge rule on port 443, got %+v", rules)
	}
}

func TestRuleResponsesUseNaivePublicPort(t *testing.T) {
	rules := BuildRuleResponses(model.Settings{}, []model.Inbound{{
		Name:      "naive",
		Protocol:  "naiveproxy",
		Transport: "tcp",
		Port:      8443,
		Enabled:   true,
		ProtocolFields: map[string]any{
			"publicPort": 9443,
		},
	}})
	found := false
	for _, r := range rules {
		if r.Port == 9443 && r.Protocol == "tcp" && r.Service == "Veil NaiveProxy" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected naiveproxy rule on public port 9443, got %+v", rules)
	}
	for _, r := range rules {
		if r.Port == 8443 && r.Service == "Veil NaiveProxy" {
			t.Fatalf("expected inbound.Port 8443 not to be opened for naiveproxy, got %+v", rules)
		}
	}
}

func TestRuleResponsesNaivePublicPortFallsBackToInboundPort(t *testing.T) {
	rules := BuildRuleResponses(model.Settings{}, []model.Inbound{{
		Name:      "naive",
		Protocol:  "naiveproxy",
		Transport: "tcp",
		Port:      8443,
		Enabled:   true,
	}})
	found := false
	for _, r := range rules {
		if r.Port == 8443 && r.Protocol == "tcp" && r.Service == "Veil NaiveProxy" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected naiveproxy rule on inbound port 8443, got %+v", rules)
	}
}
