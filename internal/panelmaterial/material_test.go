package panelmaterial

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedMaterialBuildsEnvContent(t *testing.T) {
	paths := Paths{EtcDir: filepath.Join("tmp", "etc", "veil")}
	material := NewManagedMaterial(Input{
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
		"VEIL_TLS_CERT=" + filepath.ToSlash(filepath.Join("tmp", "etc", "veil", "panel", "tls.crt")) + "\n",
		"VEIL_TLS_KEY=" + filepath.ToSlash(filepath.Join("tmp", "etc", "veil", "panel", "tls.key")) + "\n",
		"VEIL_WEB_BASE_PATH=/panel/\n",
	} {
		if !strings.Contains(env, want) {
			t.Fatalf("env missing %q:\n%s", want, env)
		}
	}
}

func TestManagedMaterialFilesIncludePanelCaddyAndSystemdMaterial(t *testing.T) {
	material := NewManagedMaterial(Input{
		Paths:             Paths{EtcDir: "/etc/veil", VarDir: "/var/lib/veil", SystemdDir: "/etc/systemd/system", VeilBinary: "/usr/local/bin/veil", CaddyBinary: "/usr/local/bin/caddy"},
		PanelAuthToken:    "token",
		PanelListen:       "127.0.0.1:2096",
		InstallPanelCaddy: true,
		Caddyfile:         "panel caddy",
	})
	files, err := material.Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	for _, want := range []string{"/etc/veil/generated/caddy/panel.Caddyfile", "/var/lib/veil/www/index.html", "/etc/veil/veil.env", "/etc/systemd/system/veil.service", "/etc/systemd/system/veil-caddy@.service"} {
		wantSlash := filepath.FromSlash(want)
		if !hasFile(files, wantSlash) {
			t.Fatalf("files missing %q (native: %q): %+v", want, wantSlash, files)
		}
	}
}

func TestManagedMaterialOmitsPanelTLSForCaddyAccess(t *testing.T) {
	material := NewManagedMaterial(Input{Paths: Paths{EtcDir: "/etc/veil"}, PanelAuthToken: "token", PanelAccess: "caddy", PanelTLSEnabled: false})
	env := material.EnvContent()
	if strings.Contains(env, "VEIL_TLS_CERT") || strings.Contains(env, "VEIL_TLS_KEY") {
		t.Fatalf("Panel Caddy access env should not include Panel TLS paths:\n%s", env)
	}
}

func hasFile(files []File, path string) bool {
	for _, file := range files {
		if file.Path == path {
			return true
		}
	}
	return false
}
