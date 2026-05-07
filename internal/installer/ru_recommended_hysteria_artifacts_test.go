package installer

import (
	"strings"
	"testing"
)

func TestRURecommendedHysteriaArtifactsBuildsPasswordConfigAndClientLink(t *testing.T) {
	artifacts, err := NewRURecommendedHysteriaArtifacts().Build(
		RURecommendedInput{Domain: "example.com", Secret: func(label string) string { return "secret-" + label }},
		SharedPortPlan{Port: 443},
		RURecommendedDefaults{MasqueradeURL: "https://www.bing.com/"},
	)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if artifacts.Password != "secret-hysteria2" || artifacts.ClientURI != "hysteria2://secret-hysteria2@example.com:443?insecure=0" {
		t.Fatalf("artifacts = %+v", artifacts)
	}
	if !strings.Contains(artifacts.ServerYAML, "listen: :443") || !strings.Contains(artifacts.ServerYAML, "https://www.bing.com/") {
		t.Fatalf("server yaml missing expected fields:\n%s", artifacts.ServerYAML)
	}
}
