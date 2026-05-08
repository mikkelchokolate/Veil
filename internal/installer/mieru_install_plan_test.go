package installer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyMieruInstallStackNormalizesToPanelOnly(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{Secret: func(label string) string { return "secret-" + label }})
	if err != nil {
		t.Fatalf("BuildRURecommendedProfile: %v", err)
	}
	if profile.Stack != StackPanel || profile.InstallMieru || profile.PortPlan.Port != 0 {
		t.Fatalf("Mieru stack should normalize to Panel install; configure Mieru as Panel Inbounds: %+v", profile)
	}
	plan, err := BuildInstallPlan(profile, InstallPlanInput{Platform: Platform{OS: "linux", Arch: "amd64"}, SystemdUnits: []string{"veil.service"}, PanelPort: 2096, MieruVersion: "v3.12.0"})
	if err != nil {
		t.Fatalf("BuildInstallPlan: %v", err)
	}
	if hasFirewallAction(plan, "443/tcp") || hasFirewallAction(plan, "443/udp") || plan.MieruBinary.Name != "" {
		t.Fatalf("Panel install should not plan Mieru runtime from legacy stack: %+v", plan)
	}
}

func TestPanelInstallDoesNotWriteMieruUnitOrProxyConfig(t *testing.T) {
	dir := t.TempDir()
	profile := RURecommendedProfile{Stack: StackPanel, PanelAuthToken: "secret-panel", PanelTLSEnabled: true, PanelTLSCertPEM: "cert", PanelTLSKeyPEM: "key"}
	result, err := ApplyRURecommendedProfile(profile, ApplyPaths{EtcDir: filepath.Join(dir, "etc", "veil"), VarDir: filepath.Join(dir, "var", "lib", "veil"), SystemdDir: filepath.Join(dir, "etc", "systemd", "system")})
	if err != nil {
		t.Fatalf("ApplyRURecommendedProfile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "etc", "systemd", "system", "veil-mieru.service")); !os.IsNotExist(err) {
		t.Fatalf("Panel install should not write Mieru unit before Panel Inbounds exist, stat err: %v", err)
	}
	if _, err := os.Stat(result.CaddyfilePath); !os.IsNotExist(err) {
		t.Fatalf("Panel install should not write Caddyfile, stat err: %v", err)
	}
	if _, err := os.Stat(result.Hysteria2Path); !os.IsNotExist(err) {
		t.Fatalf("Panel install should not write Hysteria2 config, stat err: %v", err)
	}
}
