package installer

import (
	"strings"
	"testing"
)

func TestBuildInstallPlanSummaryIncludesPanelSystemdAndFirewall(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{Secret: func(label string) string { return "secret-" + label }})
	if err != nil {
		t.Fatalf("build profile: %v", err)
	}
	plan, err := BuildInstallPlan(profile, InstallPlanInput{Platform: Platform{OS: "linux", Arch: "amd64"}, SystemdUnits: []string{"veil.service"}, PanelPort: 2096})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	text := plan.Summary()
	for _, want := range []string{"systemctl daemon-reload", "systemctl restart veil.service", "ufw allow 2096/tcp comment Veil panel", "Panel speedtest tool: speedtest-cli or speedtest"} {
		if !strings.Contains(text, want) {
			t.Fatalf("summary missing %q:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{"Shared port:", "Hysteria2 asset:", "Caddy/NaiveProxy build:", "ufw allow 443/tcp comment Veil NaiveProxy", "ufw allow 443/udp comment Veil Hysteria2"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("summary should not contain %q:\n%s", unwanted, text)
		}
	}
}

func TestBuildInstallPlanSummaryIncludesPanelCaddyAccess(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{PanelAccess: "caddy", Domain: "example.com", Email: "admin@example.com", Secret: func(label string) string { return "secret-" + label }, PanelPort: 2096})
	if err != nil {
		t.Fatalf("build profile: %v", err)
	}
	plan, err := BuildInstallPlan(profile, InstallPlanInput{Platform: Platform{OS: "linux", Arch: "amd64"}, SystemdUnits: []string{"veil.service", "veil-naive.service"}, PanelPort: 2096})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	text := plan.Summary()
	for _, want := range []string{"Caddy/Panel reverse proxy: /usr/local/bin/caddy", "ufw allow 443/tcp comment Veil panel HTTPS", "veil-naive.service"} {
		if !strings.Contains(text, want) {
			t.Fatalf("summary missing %q:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{"NaiveProxy: tcp/443", "Hysteria2: udp/443", "Shared port:"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("summary should not contain %q:\n%s", unwanted, text)
		}
	}
}
