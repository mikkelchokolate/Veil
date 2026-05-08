package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPanelCaddyInstallRendersPanelOnlyCaddyfile(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{
		Stack:       StackPanel,
		PanelAccess: "caddy",
		Domain:      "panel.example.com",
		Email:       "admin@example.com",
		PanelPort:   2096,
		Secret:      func(label string) string { return "secret-" + label },
		RandomPort:  func() int { return 31874 },
	})
	if err != nil {
		t.Fatalf("BuildRURecommendedProfile: %v", err)
	}
	if !profile.InstallPanelCaddy || profile.InstallNaive || profile.InstallHysteria2 || profile.InstallMieru {
		t.Fatalf("panel Caddy install must not install proxy runtimes: %+v", profile)
	}
	for _, want := range []string{"panel.example.com", "handle_path ", "reverse_proxy 127.0.0.1:2096"} {
		if !strings.Contains(profile.Caddyfile, want) {
			t.Fatalf("Caddyfile missing %q:\n%s", want, profile.Caddyfile)
		}
	}
	for _, unwanted := range []string{"forward_proxy", "basic_auth", "probe_resistance"} {
		if strings.Contains(profile.Caddyfile, unwanted) {
			t.Fatalf("panel Caddyfile must not contain %q:\n%s", unwanted, profile.Caddyfile)
		}
	}
}

func TestPanelCaddyInstallPlanOpensHTTPSInsteadOfPanelPort(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{Stack: StackPanel, PanelAccess: "caddy", Domain: "panel.example.com", Email: "admin@example.com", PanelPort: 2096, Secret: func(label string) string { return "secret-" + label }})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildInstallPlan(profile, InstallPlanInput{Platform: Platform{OS: "linux", Arch: "amd64"}, SystemdUnits: []string{"veil.service", "veil-naive.service"}, PanelPort: 2096})
	if err != nil {
		t.Fatalf("BuildInstallPlan: %v", err)
	}
	if hasFirewallAction(plan, "2096/tcp") {
		t.Fatalf("panel Caddy install must not expose the private Panel port: %+v", plan.FirewallActions)
	}
	if !hasFirewallAction(plan, "443/tcp") {
		t.Fatalf("panel Caddy install should open HTTPS: %+v", plan.FirewallActions)
	}
	if plan.CaddyBuild.BinaryPath != "/usr/local/bin/caddy" || !strings.Contains(plan.Summary(), "Caddy/Panel reverse proxy") {
		t.Fatalf("panel Caddy install should include standard Caddy guidance: %+v\n%s", plan.CaddyBuild, plan.Summary())
	}
}

func TestPanelCaddyInstallWritesCaddyfileAndCaddyRuntimeUnit(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{Stack: StackPanel, PanelAccess: "caddy", Domain: "panel.example.com", Email: "admin@example.com", PanelPort: 2096, Secret: func(label string) string { return "secret-" + label }})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	result, err := ApplyRURecommendedProfile(profile, ApplyPaths{EtcDir: filepath.Join(dir, "etc", "veil"), VarDir: filepath.Join(dir, "var", "lib", "veil"), SystemdDir: filepath.Join(dir, "systemd")})
	if err != nil {
		t.Fatalf("ApplyRURecommendedProfile: %v", err)
	}
	body, err := os.ReadFile(result.CaddyfilePath)
	if err != nil {
		t.Fatalf("panel Caddyfile should be written: %v", err)
	}
	if !strings.Contains(string(body), "reverse_proxy 127.0.0.1:2096") {
		t.Fatalf("unexpected panel Caddyfile:\n%s", string(body))
	}
	if _, err := os.Stat(filepath.Join(dir, "systemd", "veil-naive.service")); err != nil {
		t.Fatalf("Caddy runtime unit should be written for panel Caddy access: %v", err)
	}
}
