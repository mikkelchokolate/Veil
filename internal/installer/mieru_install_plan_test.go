package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMieruOnlyInstallPlanDoesNotOpenProxyFirewallPort(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{Stack: StackMieru, Secret: func(label string) string { return "secret-" + label }, RandomPort: func() int { return 31874 }})
	if err != nil {
		t.Fatalf("BuildRURecommendedProfile: %v", err)
	}
	plan, err := BuildInstallPlan(profile, InstallPlanInput{Platform: Platform{OS: "linux", Arch: "amd64"}, SystemdUnits: []string{"veil.service", "veil-mieru.service"}, PanelPort: 2096, MieruVersion: "v3.12.0"})
	if err != nil {
		t.Fatalf("BuildInstallPlan: %v", err)
	}
	if hasFirewallAction(plan, "443/tcp") || hasFirewallAction(plan, "443/udp") || hasFirewallAction(plan, "31874/tcp") || hasFirewallAction(plan, "31874/udp") {
		t.Fatalf("Mieru runtime-only install must not open proxy ports before Inbounds exist: %+v", plan.FirewallActions)
	}
	if !hasFirewallAction(plan, "2096/tcp") {
		t.Fatalf("expected panel firewall rule: %+v", plan.FirewallActions)
	}
}

func TestMieruOnlyInstallDoesNotRequireDomainAndPlansMieruRuntime(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{Stack: StackMieru, Secret: func(label string) string { return "secret-" + label }, RandomPort: func() int { return 31874 }})
	if err != nil {
		t.Fatalf("BuildRURecommendedProfile: %v", err)
	}
	if !profile.InstallMieru || profile.InstallNaive || profile.InstallHysteria2 {
		t.Fatalf("unexpected Mieru stack profile: %+v", profile)
	}
	plan, err := BuildInstallPlan(profile, InstallPlanInput{Platform: Platform{OS: "linux", Arch: "amd64"}, SystemdUnits: []string{"veil.service", "veil-mieru.service"}, PanelPort: 2096, MieruVersion: "v3.12.0"})
	if err != nil {
		t.Fatalf("BuildInstallPlan: %v", err)
	}
	if plan.MieruBinary.Name != "mieru" || plan.MieruBinary.Destination != "/usr/local/bin/mieru" {
		t.Fatalf("Mieru binary = %+v", plan.MieruBinary)
	}
	if !strings.Contains(plan.Summary(), "Mieru asset:") || strings.Contains(plan.Summary(), "Caddy/NaiveProxy build") {
		t.Fatalf("summary = %s", plan.Summary())
	}
}

func TestMieruOnlyInstallWritesMieruUnitButNoProxyConfig(t *testing.T) {
	dir := t.TempDir()
	profile := RURecommendedProfile{Stack: StackMieru, InstallMieru: true, PanelAuthToken: "secret-panel"}
	result, err := ApplyRURecommendedProfile(profile, ApplyPaths{EtcDir: filepath.Join(dir, "etc", "veil"), VarDir: filepath.Join(dir, "var", "lib", "veil"), SystemdDir: filepath.Join(dir, "etc", "systemd", "system")})
	if err != nil {
		t.Fatalf("ApplyRURecommendedProfile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "etc", "systemd", "system", "veil-mieru.service")); err != nil {
		t.Fatalf("expected Mieru unit: %v", err)
	}
	if _, err := os.Stat(result.CaddyfilePath); !os.IsNotExist(err) {
		t.Fatalf("Mieru-only install should not write Caddyfile, stat err: %v", err)
	}
	if _, err := os.Stat(result.Hysteria2Path); !os.IsNotExist(err) {
		t.Fatalf("Mieru-only install should not write Hysteria2 config, stat err: %v", err)
	}
}
