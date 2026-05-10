package generatedconfig

import (
	"strings"
	"testing"
)

func TestGeneratedWarpConfigRendererRendersEnabledWarpArtifact(t *testing.T) {
	renderer := NewGeneratedWarpConfigRenderer(NewGeneratedConfigPaths("/apply"))
	artifact, ok, err := renderer.Render(WarpConfig{Enabled: true, PrivateKey: "priv", PeerPublicKey: "peer", LocalAddress: "172.16.0.2/32"}, []RoutingRule{{Name: "warp", Match: "geosite:openai", Outbound: "warp", Enabled: true}})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !ok {
		t.Fatal("expected artifact")
	}
	if artifact.Path != NewGeneratedConfigPaths("/apply").Warp() {
		t.Fatalf("path = %q", artifact.Path)
	}
	if !strings.Contains(artifact.Body, "priv") || !strings.Contains(artifact.Body, "peer") {
		t.Fatalf("body missing warp credentials:\n%s", artifact.Body)
	}
}

func TestGeneratedWarpConfigRendererSkipsDisabledWarp(t *testing.T) {
	_, ok, err := NewGeneratedWarpConfigRenderer(NewGeneratedConfigPaths("/apply")).Render(WarpConfig{}, nil)
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestRenderWarpRoutingRulesUsesEnabledRulesOnly(t *testing.T) {
	rules := RenderWarpRoutingRules([]RoutingRule{
		{Match: "geoip:ru", Outbound: "direct", Enabled: true},
		{Match: "all", Outbound: "warp", Enabled: false},
	})
	if len(rules) != 1 || rules[0].Match != "geoip:ru" {
		t.Fatalf("rules = %+v", rules)
	}
}
