package cli

import (
	"strings"
	"testing"

	"github.com/veil-panel/veil/internal/installer"
)

func TestBuildRURecommendedInstallPlanSummaryUsesPanelOnlySystemdUnits(t *testing.T) {
	profile, err := installer.BuildRURecommendedProfile(installer.RURecommendedInput{
		Secret:    func(label string) string { return "secret-" + label },
		PanelPort: 2096,
	})
	if err != nil {
		t.Fatalf("BuildRURecommendedProfile: %v", err)
	}

	summary, err := buildRURecommendedInstallPlanSummary(profile, 2096, "")
	if err != nil {
		t.Fatalf("buildRURecommendedInstallPlanSummary: %v", err)
	}
	for _, want := range []string{"veil.service", "ufw allow 2096/tcp comment Veil panel"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q:\n%s", want, summary)
		}
	}
	for _, unwanted := range []string{"veil-naive.service", "veil-hysteria2.service", "/udp comment Veil Hysteria2"} {
		if strings.Contains(summary, unwanted) {
			t.Fatalf("Panel install summary should not include %q:\n%s", unwanted, summary)
		}
	}
}
