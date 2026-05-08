package installer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPanelOnlyInstallPlanDoesNotOpenProxyFirewallPort(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{Stack: StackPanel, Secret: func(label string) string { return "secret-" + label }, PanelPort: 2096, RandomPort: func() int { return 31874 }})
	if err != nil {
		t.Fatalf("BuildRURecommendedProfile panel-only: %v", err)
	}
	plan, err := BuildInstallPlan(profile, InstallPlanInput{Platform: Platform{OS: "linux", Arch: "amd64"}, SystemdUnits: []string{"veil.service"}, PanelPort: 2096})
	if err != nil {
		t.Fatalf("BuildInstallPlan: %v", err)
	}
	if hasFirewallAction(plan, "443/tcp") || hasFirewallAction(plan, "443/udp") || hasFirewallAction(plan, "31874/tcp") || hasFirewallAction(plan, "31874/udp") {
		t.Fatalf("panel-only install must not open proxy ports: %+v", plan.FirewallActions)
	}
	if !hasFirewallAction(plan, "2096/tcp") {
		t.Fatalf("expected panel firewall rule: %+v", plan.FirewallActions)
	}
}

func TestPanelOnlyInstallDoesNotRequireDomainAndWritesNoProxyConfigs(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{
		Stack:      StackPanel,
		Secret:     func(label string) string { return "secret-" + label },
		PanelPort:  2096,
		RandomPort: func() int { return 31874 },
	})
	if err != nil {
		t.Fatalf("BuildRURecommendedProfile panel-only: %v", err)
	}
	if profile.InstallNaive || profile.InstallHysteria2 || profile.Caddyfile != "" || profile.Hysteria2YAML != "" || profile.WebBasePath != "" {
		t.Fatalf("panel-only profile should not include proxy artifacts: %+v", profile)
	}
	dir := t.TempDir()
	result, err := ApplyRURecommendedProfile(profile, ApplyPaths{
		EtcDir:     filepath.Join(dir, "etc", "veil"),
		VarDir:     filepath.Join(dir, "var", "lib", "veil"),
		SystemdDir: filepath.Join(dir, "etc", "systemd", "system"),
	})
	if err != nil {
		t.Fatalf("ApplyRURecommendedProfile: %v", err)
	}
	if _, err := os.Stat(result.CaddyfilePath); !os.IsNotExist(err) {
		t.Fatalf("panel-only install should not write Caddyfile, stat err: %v", err)
	}
	if _, err := os.Stat(result.Hysteria2Path); !os.IsNotExist(err) {
		t.Fatalf("panel-only install should not write Hysteria2 config, stat err: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "etc", "systemd", "system", "veil.service")); err != nil {
		t.Fatalf("panel-only install should write veil.service: %v", err)
	}
}
