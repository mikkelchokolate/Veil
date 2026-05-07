package cli

import (
	"strings"
	"testing"

	"github.com/veil-panel/veil/internal/installer"
)

func TestBuildRURecommendedInstallPlanSummaryIncludesSelectedSystemdUnits(t *testing.T) {
	profile, err := installer.BuildRURecommendedProfile(installer.RURecommendedInput{
		Domain:       "example.com",
		Email:        "admin@example.com",
		Stack:        installer.StackHysteria2,
		Port:         31874,
		Availability: installer.PortAvailability{TCPBusy: map[int]bool{}, UDPBusy: map[int]bool{}},
		Secret:       func(label string) string { return "secret-" + label },
		RandomPort:   func() int { return 31874 },
		PanelPort:    2096,
	})
	if err != nil {
		t.Fatalf("BuildRURecommendedProfile: %v", err)
	}

	summary, err := buildRURecommendedInstallPlanSummary(profile, 2096, "")
	if err != nil {
		t.Fatalf("buildRURecommendedInstallPlanSummary: %v", err)
	}
	for _, want := range []string{"veil.service", "veil-hysteria2.service", "ufw allow ", "/udp comment Veil Hysteria2"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q:\n%s", want, summary)
		}
	}
	if strings.Contains(summary, "veil-naive.service") {
		t.Fatalf("summary should not include naive unit for hysteria2-only install:\n%s", summary)
	}
}
