package api

import "testing"

func TestInboundApplyArtifactsReturnsProtocolConfigsAndActions(t *testing.T) {
	artifacts := NewInboundApplyArtifacts()
	cases := []struct {
		protocol string
		config   string
		action   string
		validate bool
	}{
		{"naiveproxy", "/etc/veil/generated/caddy/Caddyfile", "reload veil-naive.service", true},
		{"hysteria2", "/etc/veil/generated/hysteria2/server.yaml", "reload veil-hysteria2.service", true},
		{"mieru", "/etc/veil/generated/mieru/server_config.json", "restart veil-mieru.service", false},
	}
	for _, tc := range cases {
		artifact, ok := artifacts.ForProtocol(tc.protocol)
		if !ok {
			t.Fatalf("%s should be supported", tc.protocol)
		}
		if artifact.Config != tc.config || artifact.Action != tc.action || artifact.ValidateInboundRender != tc.validate {
			t.Fatalf("artifact for %s = %+v", tc.protocol, artifact)
		}
	}
	if artifact, ok := artifacts.ForProtocol("unknown"); ok || artifact != (InboundApplyArtifact{}) {
		t.Fatalf("unknown artifact = %+v %v", artifact, ok)
	}
}
