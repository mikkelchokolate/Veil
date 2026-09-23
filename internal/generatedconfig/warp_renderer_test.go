package generatedconfig

import (
	"encoding/json"
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
	// Structural assertions, not bare substring: the credentials must land in
	// the wireguard endpoint fields, and the geosite rule must emit a remote
	// rule_set with its URL and the warp final route (#822).
	var doc struct {
		Endpoints []struct {
			Type       string `json:"type"`
			Tag        string `json:"tag"`
			PrivateKey string `json:"private_key"`
			Peers      []struct {
				PublicKey string `json:"public_key"`
			} `json:"peers"`
		} `json:"endpoints"`
		Route struct {
			Final string `json:"final"`
			Rules []struct {
				Outbound string `json:"outbound"`
				RuleSet  string `json:"rule_set"`
			} `json:"rules"`
			RuleSet []struct {
				Tag  string `json:"tag"`
				Type string `json:"type"`
				URL  string `json:"url"`
			} `json:"rule_set"`
		} `json:"route"`
	}
	if err := json.Unmarshal([]byte(artifact.Body), &doc); err != nil {
		t.Fatalf("warp artifact is not JSON: %v\n%s", err, artifact.Body)
	}
	if len(doc.Endpoints) != 1 || doc.Endpoints[0].Type != "wireguard" || doc.Endpoints[0].Tag != "warp" {
		t.Fatalf("endpoints = %+v, want one wireguard warp endpoint", doc.Endpoints)
	}
	if doc.Endpoints[0].PrivateKey != "priv" || len(doc.Endpoints[0].Peers) != 1 || doc.Endpoints[0].Peers[0].PublicKey != "peer" {
		t.Fatalf("endpoint credentials = %+v, want private_key=priv peer public_key=peer", doc.Endpoints[0])
	}
	if doc.Route.Final != "warp" {
		t.Fatalf("route.final = %q, want warp", doc.Route.Final)
	}
	if len(doc.Route.Rules) != 1 || doc.Route.Rules[0].RuleSet != "geosite-openai" || doc.Route.Rules[0].Outbound != "warp" {
		t.Fatalf("route.rules = %+v, want one geosite-openai→warp rule_set rule", doc.Route.Rules)
	}
	if len(doc.Route.RuleSet) != 1 || doc.Route.RuleSet[0].Tag != "geosite-openai" || doc.Route.RuleSet[0].Type != "remote" ||
		doc.Route.RuleSet[0].URL != "https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-openai.srs" {
		t.Fatalf("route.rule_set = %+v, want remote geosite-openai srs", doc.Route.RuleSet)
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
