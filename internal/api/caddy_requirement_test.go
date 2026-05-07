package api

import "testing"

func TestCaddyRequirementDependsOnNaiveInboundOrPanelHTTPSAccess(t *testing.T) {
	policy := NewCaddyRequirement()
	if policy.Required(Settings{}, nil) {
		t.Fatal("empty Panel should not require Caddy")
	}
	if policy.Required(Settings{}, []Inbound{{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true}}) {
		t.Fatal("Mieru-only Inbounds should not require Caddy")
	}
	if policy.Required(Settings{}, []Inbound{{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true}}) {
		t.Fatal("Hysteria2-only Inbounds should not require Caddy")
	}
	if !policy.Required(Settings{}, []Inbound{{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true}}) {
		t.Fatal("NaiveProxy Inbound should require Caddy")
	}
	if !policy.Required(Settings{PanelAccess: "caddy"}, nil) {
		t.Fatal("Panel access through Caddy should require Caddy")
	}
}
