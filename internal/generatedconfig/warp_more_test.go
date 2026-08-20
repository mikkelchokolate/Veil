package generatedconfig

import (
	"strings"
	"testing"
)

func TestRenderWarpRoutingRulesKeepsProxyOffWarp(t *testing.T) {
	rules := RenderWarpRoutingRules([]RoutingRule{
		{Match: "all", Outbound: "proxy", Enabled: true},
		{Match: "geosite:openai", Outbound: "warp", Enabled: true},
		{Match: "geoip:private", Outbound: "direct", Enabled: true},
	})
	if len(rules) != 3 {
		t.Fatalf("rules = %+v", rules)
	}
	if rules[0].Outbound != "proxy" {
		t.Fatalf("proxy must not be rewritten to warp: %+v", rules[0])
	}
	if rules[1].Outbound != "warp" || rules[2].Outbound != "direct" {
		t.Fatalf("warp/direct tags = %+v", rules)
	}
}

func TestGeneratedWarpConfigRendererReturnsRenderError(t *testing.T) {
	renderer := NewGeneratedWarpConfigRenderer(NewGeneratedConfigPaths("/apply"))
	_, _, err := renderer.Render(WarpConfig{Enabled: true, PrivateKey: "", LocalAddress: "172.16.0.2/32", PeerPublicKey: "peer"}, nil)
	if err == nil || !strings.Contains(err.Error(), "private key is required") {
		t.Fatalf("expected private key error, got %v", err)
	}
}

func TestGeneratedWarpConfigRendererReturnsErrorForInvalidEndpointPort(t *testing.T) {
	renderer := NewGeneratedWarpConfigRenderer(NewGeneratedConfigPaths("/apply"))
	_, _, err := renderer.Render(WarpConfig{
		Enabled:       true,
		PrivateKey:    "priv",
		LocalAddress:  "172.16.0.2/32",
		PeerPublicKey: "peer",
		Endpoint:      "engage.cloudflareclient.com:99999",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "port must be between 1 and 65535") {
		t.Fatalf("expected endpoint port error, got %v", err)
	}
}
