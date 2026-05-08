package api

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestManagementConfigRendererBuildsGeneratedConfigSet(t *testing.T) {
	root := t.TempDir()
	renderer := NewManagementConfigRenderer(ManagementConfigInput{
		ApplyRoot: root,
		Settings:  Settings{Domain: "vpn.example.com", Email: "admin@example.com", Stack: "both", NaiveUsername: "veil", NaivePassword: "naive-pass", Hysteria2Password: "hy2-pass"},
		Inbounds:  []Inbound{{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true}},
	})

	configs, err := renderer.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	body := configs[filepath.Join(root, "generated", "caddy", "Caddyfile")]
	if !strings.Contains(body, "vpn.example.com") {
		t.Fatalf("Caddyfile missing domain: %s", body)
	}
}

func TestManagementConfigRendererValidatesMieruInboundRender(t *testing.T) {
	renderer := NewManagementConfigRenderer(ManagementConfigInput{Settings: Settings{Stack: "mieru"}})

	_, err := renderer.RenderInbound(Inbound{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true})

	if err == nil || err.Error() != "mieru user name and password are required" {
		t.Fatalf("expected Mieru render validation error, got %v", err)
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
