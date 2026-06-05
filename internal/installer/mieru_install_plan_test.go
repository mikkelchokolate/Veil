package installer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/hostenv"
)

func TestPanelInstallDoesNotPlanMieruRuntime(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{Secret: func(label string) string { return "secret-" + label }})
	if err != nil {
		t.Fatalf("BuildRURecommendedProfile: %v", err)
	}
	if profile.InstallPanelCaddy {
		t.Fatalf("Panel-only install should keep Mieru runtime under Panel Inbounds: %+v", profile)
	}
	plan, err := BuildInstallPlan(profile, InstallPlanInput{Platform: hostenv.Platform{OS: "linux", Arch: "amd64"}, SystemdUnits: []string{"veil.service"}, PanelPort: 2096})
	if err != nil {
		t.Fatalf("BuildInstallPlan: %v", err)
	}
	if hasFirewallAction(plan, "443/tcp") || hasFirewallAction(plan, "443/udp") {
		t.Fatalf("Panel install should not plan Mieru runtime before Panel Inbounds exist: %+v", plan)
	}
}

func TestPanelInstallWritesDormantManagedRuntimeUnitsWithoutProxyConfig(t *testing.T) {
	dir := t.TempDir()
	profile := RURecommendedProfile{PanelAuthToken: "secret-panel", PanelTLSEnabled: true, PanelTLSCertPEM: "cert", PanelTLSKeyPEM: "key"}
	result, err := ApplyRURecommendedProfile(profile, ApplyPaths{EtcDir: filepath.Join(dir, "etc", "veil"), VarDir: filepath.Join(dir, "var", "lib", "veil"), SystemdDir: filepath.Join(dir, "etc", "systemd", "system")})
	if err != nil {
		t.Fatalf("ApplyRURecommendedProfile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "etc", "systemd", "system", "veil-mieru.service")); err != nil {
		t.Fatalf("Panel install should write dormant Mieru unit template, stat err: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "etc", "systemd", "system", "veil-hysteria2@.service")); err != nil {
		t.Fatalf("Panel install should write dormant Hysteria2 unit template, stat err: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "etc", "systemd", "system", "veil-olcrtc@.service")); err != nil {
		t.Fatalf("Panel install should write dormant olcRTC unit template, stat err: %v", err)
	}
	if _, err := os.Stat(result.CaddyfilePath); !os.IsNotExist(err) {
		t.Fatalf("Panel install should not write Caddyfile, stat err: %v", err)
	}
	if _, err := os.Stat(result.Hysteria2Path); !os.IsNotExist(err) {
		t.Fatalf("Panel install should not write Hysteria2 config, stat err: %v", err)
	}
}
