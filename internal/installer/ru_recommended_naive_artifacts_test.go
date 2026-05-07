package installer

import (
	"strings"
	"testing"
)

func TestRURecommendedNaiveArtifactsBuildsPasswordConfigAndClientLink(t *testing.T) {
	artifacts, err := NewRURecommendedNaiveArtifacts().Build(
		RURecommendedInput{Domain: "example.com", Email: "admin@example.com", PanelPort: 2096, Secret: func(label string) string { return "secret-" + label }},
		SharedPortPlan{Port: 443},
		RURecommendedDefaults{Username: "veil", FallbackRoot: "/var/lib/veil/www"},
		"/panelpath/",
	)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if artifacts.Password != "secret-naive" || artifacts.ClientURL != "https://veil:secret-naive@example.com:443" {
		t.Fatalf("artifacts = %+v", artifacts)
	}
	if !strings.Contains(artifacts.Caddyfile, "reverse_proxy 127.0.0.1:2096") || !strings.Contains(artifacts.Caddyfile, "handle_path /panelpath/*") {
		t.Fatalf("caddyfile missing panel routing:\n%s", artifacts.Caddyfile)
	}
}
