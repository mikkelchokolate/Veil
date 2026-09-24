package installer

import (
	"os"
	"path/filepath"
	"strings"
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
	plan, err := BuildInstallPlan(profile, InstallPlanInput{Platform: hostenv.Platform{OS: "linux", Arch: "amd64"}, SystemdUnits: []string{"veil.service"}, PanelAccess: profile.PanelAccess, PanelPort: 2096})
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
		t.Fatalf("Panel install should not write Caddy config, stat err: %v", err)
	}
	if _, err := os.Stat(result.Hysteria2Path); !os.IsNotExist(err) {
		t.Fatalf("Panel install should not write Hysteria2 config, stat err: %v", err)
	}
}

// The dormant mieru unit is a hardening contract, not just a file on disk:
// it must run as the dedicated veil-mita identity with only the veil-proxy
// supplementary group it needs to read generated configs (#624). Kept
// separate from the dormant-presence test so a content regression cannot be
// masked by a missing-file failure.
func TestDormantMieruUnitRunsAsDedicatedMitaIdentity(t *testing.T) {
	dir := t.TempDir()
	profile := RURecommendedProfile{PanelAuthToken: "secret-panel", PanelTLSEnabled: true, PanelTLSCertPEM: "cert", PanelTLSKeyPEM: "key"}
	systemdDir := filepath.Join(dir, "etc", "systemd", "system")
	if _, err := ApplyRURecommendedProfile(profile, ApplyPaths{EtcDir: filepath.Join(dir, "etc", "veil"), VarDir: filepath.Join(dir, "var", "lib", "veil"), SystemdDir: systemdDir}); err != nil {
		t.Fatalf("ApplyRURecommendedProfile: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(systemdDir, "veil-mieru.service"))
	if err != nil {
		t.Fatalf("read veil-mieru.service: %v", err)
	}
	for _, want := range []string{
		"User=veil-mita\n",
		"Group=veil-mita\n",
		"SupplementaryGroups=veil-proxy\n",
		"RuntimeDirectory=veil-mieru\n",
		"RuntimeDirectoryMode=0750\n",
		"UMask=0007\n",
	} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("veil-mieru.service missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(string(body), "User=veil-proxy") {
		t.Fatalf("veil-mieru.service must not run as the shared veil-proxy edge identity:\n%s", body)
	}
}

// The Caddy panel profile is exercised apart from the mieru-hardening test:
// a caddy-access install must render the caddy unit and its config instead of
// the dormant panel-TLS material.
func TestCaddyPanelProfileInstallsCaddyUnitAndConfig(t *testing.T) {
	dir := t.TempDir()
	profile := RURecommendedProfile{
		PanelAuthToken:    "secret-panel",
		PanelAccess:       "caddy",
		InstallPanelCaddy: true,
		Domain:            "vpn.example.com",
		Email:             "admin@example.com",
		CaddyJSON:         "{}",
	}
	systemdDir := filepath.Join(dir, "etc", "systemd", "system")
	if _, err := ApplyRURecommendedProfile(profile, ApplyPaths{EtcDir: filepath.Join(dir, "etc", "veil"), VarDir: filepath.Join(dir, "var", "lib", "veil"), SystemdDir: systemdDir}); err != nil {
		t.Fatalf("ApplyRURecommendedProfile: %v", err)
	}
	caddyUnit, err := os.ReadFile(filepath.Join(systemdDir, "veil-caddy.service"))
	if err != nil {
		t.Fatalf("caddy panel profile must write veil-caddy.service: %v", err)
	}
	for _, want := range []string{"User=veil-proxy\n", "ExecStart=", "CapabilityBoundingSet=CAP_NET_BIND_SERVICE\n"} {
		if !strings.Contains(string(caddyUnit), want) {
			t.Fatalf("veil-caddy.service missing %q:\n%s", want, caddyUnit)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "etc", "veil", "generated", "caddy", "config.json")); err != nil {
		t.Fatalf("caddy panel profile must render the caddy config: %v", err)
	}
}
