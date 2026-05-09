package panelaccess

import (
	"strings"
	"testing"
)

func TestProfileBuildsPanelCaddyAccessMaterial(t *testing.T) {
	material, err := NewProfile(ProfileInput{PanelAccess: "caddy", Domain: "panel.example.com", Email: "admin@example.com", PanelPort: 2096}).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if material.PanelListen != "127.0.0.1:2096" || !material.InstallPanelCaddy || material.PanelTLSEnabled {
		t.Fatalf("material = %+v", material)
	}
	if material.WebBasePath == "" || !strings.HasPrefix(material.WebBasePath, "/") || !strings.HasSuffix(material.WebBasePath, "/") {
		t.Fatalf("web base path = %q", material.WebBasePath)
	}
	for _, want := range []string{"panel.example.com", "reverse_proxy 127.0.0.1:2096", "handle_path " + strings.TrimRight(material.WebBasePath, "/") + "/*"} {
		if !strings.Contains(material.Caddyfile, want) {
			t.Fatalf("Caddyfile missing %q:\n%s", want, material.Caddyfile)
		}
	}
}

func TestProfileBuildsLocalPanelTLSMaterial(t *testing.T) {
	material, err := NewProfile(ProfileInput{PanelAccess: "local", Domain: "panel.example.com", PanelPort: 2096}).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if material.PanelListen != "127.0.0.1:2096" || !material.PanelTLSEnabled || material.InstallPanelCaddy {
		t.Fatalf("material = %+v", material)
	}
	if !strings.Contains(material.PanelTLSCertPEM, "BEGIN CERTIFICATE") || !strings.Contains(material.PanelTLSKeyPEM, "BEGIN RSA PRIVATE KEY") {
		t.Fatalf("tls material missing PEM blocks")
	}
}

func TestProfileBuildsDirectListenAddress(t *testing.T) {
	material, err := NewProfile(ProfileInput{PanelAccess: "direct", PanelPort: 9443}).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if material.PanelListen != "0.0.0.0:9443" {
		t.Fatalf("PanelListen = %q", material.PanelListen)
	}
}
