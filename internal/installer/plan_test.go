package installer

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/hostenv"
)

func TestBuildInstallPlanSummaryIncludesPanelSystemdNoLocalFirewall(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{Secret: func(label string) string { return "secret-" + label }})
	if err != nil {
		t.Fatalf("build profile: %v", err)
	}
	plan, err := BuildInstallPlan(profile, InstallPlanInput{Platform: hostenv.Platform{OS: "linux", Arch: "amd64"}, SystemdUnits: []string{"veil.service"}, PanelAccess: profile.PanelAccess, PanelPort: 2096})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	text := plan.Summary()
	for _, want := range []string{"systemctl daemon-reload", "systemctl restart veil.service", "Panel speedtest tool: speedtest-cli or speedtest"} {
		if !strings.Contains(text, want) {
			t.Fatalf("summary missing %q:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{"ufw allow 2096/tcp comment Veil panel", "Shared port:", "Hysteria2 asset:", "Caddy/NaiveProxy build:", "ufw allow 443/tcp comment Veil NaiveProxy", "ufw allow 443/udp comment Veil Hysteria2"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("summary should not contain %q:\n%s", unwanted, text)
		}
	}
}

func TestBuildInstallPlanSummaryDirectIncludesPanelFirewall(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{PanelAccess: "direct", Secret: func(label string) string { return "secret-" + label }, PanelPort: 2096})
	if err != nil {
		t.Fatalf("build profile: %v", err)
	}
	plan, err := BuildInstallPlan(profile, InstallPlanInput{Platform: hostenv.Platform{OS: "linux", Arch: "amd64"}, SystemdUnits: []string{"veil.service"}, PanelAccess: profile.PanelAccess, PanelPort: 2096})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	text := plan.Summary()
	if !strings.Contains(text, "ufw allow 2096/tcp comment Veil panel") {
		t.Fatalf("direct mode summary missing panel firewall rule:\n%s", text)
	}
}

func TestBuildInstallPlanSummaryIncludesPanelCaddyAccess(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{PanelAccess: "caddy", Domain: "example.com", Email: "admin@example.com", Secret: func(label string) string { return "secret-" + label }, PanelPort: 2096})
	if err != nil {
		t.Fatalf("build profile: %v", err)
	}
	plan, err := BuildInstallPlan(profile, InstallPlanInput{Platform: hostenv.Platform{OS: "linux", Arch: "amd64"}, SystemdUnits: []string{"veil.service", "veil-naive.service"}, PanelPort: 2096})
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
