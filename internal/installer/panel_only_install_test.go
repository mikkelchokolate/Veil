package installer

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/veil-panel/veil/internal/hostenv"
)

func TestPanelOnlyInstallPlanDoesNotOpenProxyFirewallPort(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{Secret: func(label string) string { return "secret-" + label }, PanelPort: 2096})
	if err != nil {
		t.Fatalf("BuildRURecommendedProfile panel-only: %v", err)
	}
	plan, err := BuildInstallPlan(profile, InstallPlanInput{Platform: hostenv.Platform{OS: "linux", Arch: "amd64"}, SystemdUnits: []string{"veil.service"}, PanelPort: 2096})
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

func TestPanelOnlyInstallWritesSelfSignedPanelTLS(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{Secret: func(label string) string { return "secret-" + label }, PanelPort: 2096})
	if err != nil {
		t.Fatalf("BuildRURecommendedProfile panel-only: %v", err)
	}
	dir := t.TempDir()
	_, err = ApplyRURecommendedProfile(profile, ApplyPaths{EtcDir: filepath.Join(dir, "etc", "veil"), VarDir: filepath.Join(dir, "var", "lib", "veil"), SystemdDir: filepath.Join(dir, "systemd")})
	if err != nil {
		t.Fatalf("ApplyRURecommendedProfile: %v", err)
	}
	certPath := filepath.Join(dir, "etc", "veil", "panel", "tls.crt")
	keyPath := filepath.Join(dir, "etc", "veil", "panel", "tls.key")
	envBody, err := os.ReadFile(filepath.Join(dir, "etc", "veil", "veil.env"))
	if err != nil {
		t.Fatalf("read veil.env: %v", err)
	}
	for _, want := range []string{"VEIL_TLS_CERT=" + filepath.ToSlash(certPath), "VEIL_TLS_KEY=" + filepath.ToSlash(keyPath)} {
		if !strings.Contains(string(envBody), want) {
			t.Fatalf("veil.env missing %q:\n%s", want, string(envBody))
		}
	}
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read panel TLS cert: %v", err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read panel TLS key: %v", err)
	}
	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		t.Fatalf("panel TLS key pair should be valid: %v", err)
	}
}

func TestPanelOnlyInstallDoesNotRequireDomainAndWritesNoProxyConfigs(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{
		Secret:    func(label string) string { return "secret-" + label },
		PanelPort: 2096,
	})
	if err != nil {
		t.Fatalf("BuildRURecommendedProfile panel-only: %v", err)
	}
	if profile.InstallPanelCaddy || profile.Caddyfile != "" || profile.WebBasePath != "" {
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
