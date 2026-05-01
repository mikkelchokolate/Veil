package installer

import (
	"strings"
	"testing"
)

func TestBuildInstallPlanSummaryIncludesBinariesAndSystemd(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{
		Domain:       "example.com",
		Email:        "admin@example.com",
		Availability: PortAvailability{TCPBusy: map[int]bool{}, UDPBusy: map[int]bool{}},
		Secret:       func(label string) string { return "secret-" + label },
		RandomPort:   func() int { return 31874 },
	})
	if err != nil {
		t.Fatalf("build profile: %v", err)
	}
	plan, err := BuildInstallPlan(profile, InstallPlanInput{
		Platform:        Platform{OS: "linux", Arch: "amd64"},
		HysteriaVersion: "v2.6.0",
		SystemdUnits:    []string{"veil.service", "veil-naive.service", "veil-hysteria2.service"},
	})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	text := plan.Summary()
	for _, want := range []string{
		"Shared port: 443",
		"Hysteria2 asset: https://github.com/apernet/hysteria/releases/download/app%2Fv2.6.0/hysteria-linux-amd64",
		"Caddy/NaiveProxy build: /usr/local/bin/caddy",
		"systemctl daemon-reload",
		"systemctl restart veil-hysteria2.service",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("summary missing %q:\n%s", want, text)
		}
	}
}
