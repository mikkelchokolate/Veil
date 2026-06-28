package cli

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/installer"
)

func TestBuildRURecommendedInstallPlanSummaryUsesPanelOnlySystemdUnits(t *testing.T) {
	profile, err := installer.BuildRURecommendedProfile(installer.RURecommendedInput{
		Secret:    func(label string) string { return "secret-" + label },
		PanelPort: 2096,
	})
	if err != nil {
		t.Fatalf("BuildRURecommendedProfile: %v", err)
	}

	summary, err := buildRURecommendedInstallPlanSummary(profile, 2096)
	if err != nil {
		t.Fatalf("buildRURecommendedInstallPlanSummary: %v", err)
	}
	for _, want := range []string{"veil.service"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q:\n%s", want, summary)
		}
	}
	for _, unwanted := range []string{"ufw allow 2096/tcp comment Veil panel", "veil-caddy@.service", "veil-hysteria2@.service", "/udp comment Veil Hysteria2"} {
		if strings.Contains(summary, unwanted) {
			t.Fatalf("Panel install summary should not include %q:\n%s", unwanted, summary)
		}
	}
}

func TestBuildRURecommendedInstallPlanSummaryDirectIncludesFirewallRule(t *testing.T) {
	profile, err := installer.BuildRURecommendedProfile(installer.RURecommendedInput{
		PanelAccess: "direct",
		Secret:      func(label string) string { return "secret-" + label },
		PanelPort:   2096,
	})
	if err != nil {
		t.Fatalf("BuildRURecommendedProfile: %v", err)
	}

	summary, err := buildRURecommendedInstallPlanSummary(profile, 2096)
	if err != nil {
		t.Fatalf("buildRURecommendedInstallPlanSummary: %v", err)
	}
	if !strings.Contains(summary, "ufw allow 2096/tcp comment Veil panel") {
		t.Fatalf("direct mode summary missing panel firewall rule:\n%s", summary)
	}
}
