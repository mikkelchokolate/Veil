package installer

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPanelManagedMaterialBuildsEnvContent(t *testing.T) {
	paths := ApplyPaths{EtcDir: filepath.Join("tmp", "etc", "veil")}
	material := NewPanelManagedMaterial(PanelManagedMaterialInput{
		Paths:           paths,
		PanelAuthToken:  "token",
		PanelListen:     "127.0.0.1:2096",
		PanelAccess:     "local",
		Domain:          "vpn.example.com",
		Email:           "admin@example.com",
		WebBasePath:     "/panel/",
		PanelTLSEnabled: true,
	})

	env := material.EnvContent()
	for _, want := range []string{
		"VEIL_API_TOKEN=token\n",
		"VEIL_LISTEN=127.0.0.1:2096\n",
		"VEIL_PANEL_ACCESS=local\n",
		"VEIL_DOMAIN=vpn.example.com\n",
		"VEIL_EMAIL=admin@example.com\n",
		"VEIL_TLS_CERT=" + filepath.Join("tmp", "etc", "veil", "panel", "tls.crt") + "\n",
		"VEIL_TLS_KEY=" + filepath.Join("tmp", "etc", "veil", "panel", "tls.key") + "\n",
		"VEIL_WEB_BASE_PATH=/panel/\n",
	} {
		if !strings.Contains(env, want) {
			t.Fatalf("env missing %q:\n%s", want, env)
		}
	}
}

func TestPanelManagedMaterialOmitsPanelTLSForCaddyAccess(t *testing.T) {
	material := NewPanelManagedMaterial(PanelManagedMaterialInput{Paths: ApplyPaths{EtcDir: "/etc/veil"}, PanelAuthToken: "token", PanelAccess: "caddy", PanelTLSEnabled: false})
	env := material.EnvContent()
	if strings.Contains(env, "VEIL_TLS_CERT") || strings.Contains(env, "VEIL_TLS_KEY") {
		t.Fatalf("Panel Caddy access env should not include Panel TLS paths:\n%s", env)
	}
}
