package panelaccess

import (
	"strings"
	"testing"
)

func TestPanelAccessModeBuildsListenAddressAndCaddyRequirement(t *testing.T) {
	for _, tc := range []struct {
		mode       string
		port       int
		wantListen string
		wantCaddy  bool
	}{
		{"direct", 2096, "0.0.0.0:2096", false},
		{"local", 2096, "127.0.0.1:2096", false},
		{"caddy", 2096, "127.0.0.1:2096", true},
		{"", 2096, "127.0.0.1:2096", false},
	} {
		mode, err := NewMode(tc.mode).Resolve(tc.port)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", tc.mode, err)
		}
		if mode.PanelListen != tc.wantListen || mode.RequiresCaddy != tc.wantCaddy {
			t.Fatalf("Resolve(%q) = %+v", tc.mode, mode)
		}
	}
}

func TestPanelAccessModeRejectsUnknownMode(t *testing.T) {
	_, err := NewMode("public").Resolve(2096)
	if err == nil || err.Error() != "panel access must be direct, local, or caddy" {
		t.Fatalf("err = %v", err)
	}
}

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
