package installer

import (
	"strings"
	"testing"
)

func TestBuildRURecommendedInstallSelectsPanelPortBeforeProfile(t *testing.T) {
	result, err := BuildRURecommendedInstall(RURecommendedInstallInput{
		Domain:      "example.com",
		Email:       "admin@example.com",
		Stack:       StackPanel,
		PanelAccess: "caddy",
		PanelPort:   0,
		Secret:      func(label string) string { return "secret-" + label },
		RandomPanelPort: func() (int, error) {
			return 2096, nil
		},
	})
	if err != nil {
		t.Fatalf("BuildRURecommendedInstall: %v", err)
	}
	if result.PanelPort != 2096 || !result.PanelRandom {
		t.Fatalf("panel selection = port %d random %v", result.PanelPort, result.PanelRandom)
	}
	if !strings.Contains(result.Profile.Caddyfile, "reverse_proxy 127.0.0.1:2096") {
		t.Fatalf("profile Caddyfile did not receive selected panel port:\n%s", result.Profile.Caddyfile)
	}
}
