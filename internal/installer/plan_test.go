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
		PanelPort:       2096,
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
		"ufw allow 443/tcp comment Veil NaiveProxy",
		"ufw allow 443/udp comment Veil Hysteria2",
		"ufw allow 2096/tcp comment Veil panel",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("summary missing %q:\n%s", want, text)
		}
	}
}

func TestBuildInstallPlanSummaryHonorsSelectedStack(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{
		Domain:       "example.com",
		Email:        "admin@example.com",
		Stack:        StackHysteria2,
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
		SystemdUnits:    []string{"veil.service", "veil-hysteria2.service"},
		PanelPort:       2096,
	})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	text := plan.Summary()
	for _, want := range []string{
		"Hysteria2: udp/443",
		"Hysteria2 asset:",
		"ufw allow 443/udp comment Veil Hysteria2",
		"ufw allow 2096/tcp comment Veil panel",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("summary missing %q:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{
		"NaiveProxy: tcp/443",
		"Caddy/NaiveProxy build:",
		"ufw allow 443/tcp comment Veil NaiveProxy",
		"veil-naive.service",
	} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("summary should not contain %q:\n%s", unwanted, text)
		}
	}
}
