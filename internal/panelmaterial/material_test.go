package panelmaterial

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/systemdunits"
)

func TestManagedMaterialBuildsEnvContent(t *testing.T) {
	paths := Paths{EtcDir: filepath.Join("tmp", "etc", "veil"), VarDir: filepath.Join("tmp", "var", "lib", "veil")}
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
		"VEIL_STATE_PATH=" + filepath.ToSlash(filepath.Join("tmp", "var", "lib", "veil", "state.json")) + "\n",
		"VEIL_KEY_PATH=" + filepath.ToSlash(filepath.Join("tmp", "etc", "veil", "state.key")) + "\n",
		"VEIL_APPLY_ROOT=" + filepath.ToSlash(filepath.Join("tmp", "var", "lib", "veil", "staging")) + "\n",
		"VEIL_LIVE_ROOT=" + filepath.ToSlash(filepath.Join("tmp", "etc", "veil", "generated")) + "\n",
	} {
		if !strings.Contains(env, want) {
			t.Fatalf("env missing %q:\n%s", want, env)
		}
	}
}

func TestManagedMaterialFilesIncludePanelCaddyAndSystemdMaterial(t *testing.T) {
	material := NewManagedMaterial(Input{
		Paths:             Paths{EtcDir: "/etc/veil", VarDir: "/var/lib/veil", SystemdDir: "/tmp/veil-systemd", VeilBinary: "/usr/local/bin/veil", CaddyBinary: "/usr/local/bin/caddy"},
		PanelAuthToken:    "token",
		PanelListen:       "127.0.0.1:2096",
		InstallPanelCaddy: true,
		CaddyJSON:         "{}",
	})
	files, err := material.Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	wants := []string{"/etc/veil/generated/caddy/config.json", "/etc/veil/www/index.html", "/etc/veil/veil.env"}
	for _, name := range systemdunits.Names() {
		wants = append(wants, "/tmp/veil-systemd/"+name)
	}
	for _, want := range wants {
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

func TestManagedMaterialSkipsPackagedUnitsAndWritesDropIns(t *testing.T) {
	vendor := t.TempDir()
	etcSystemd := filepath.Join(t.TempDir(), "etc", "systemd", "system")
	for _, name := range systemdunits.Names() {
		if err := os.WriteFile(filepath.Join(vendor, name), []byte("[Unit]\nDescription=vendor\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	material := NewManagedMaterial(Input{
		Paths: Paths{
			EtcDir:           "/opt/veil/etc",
			VarDir:           "/opt/veil/var",
			SystemdDir:       etcSystemd,
			VendorSystemdDir: vendor,
			VeilBinary:       "/usr/local/bin/veil",
		},
		PanelAuthToken: "token",
		PanelListen:    "127.0.0.1:2096",
	})
	files, err := material.Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if hasFile(files, filepath.Join(etcSystemd, "veil.service")) {
		t.Fatal("packaged veil.service must not be shadowed in /etc/systemd/system")
	}
	dropIn := filepath.Join(etcSystemd, "veil.service.d", "10-veil-install.conf")
	if !hasFile(files, dropIn) {
		t.Fatalf("custom paths should write a drop-in, got %+v", files)
	}
	body := fileContent(files, dropIn)
	for _, want := range []string{"VEIL_STATE_PATH=/opt/veil/var/state.json", "VEIL_KEY_PATH=/opt/veil/etc/state.key", "ReadWritePaths=/opt/veil/var"} {
		if !strings.Contains(body, want) {
			t.Fatalf("drop-in missing %q:\n%s", want, body)
		}
	}
}

// Issue #625: on a packaged host a custom --etc-dir/--var-dir install must
// drop in overrides for the protocol units too — not only panel/helper/
// backup/caddy — or they keep reading /etc/veil/generated and masking the
// wrong state tree.
func TestManagedMaterialCustomPathsWriteProtocolDropIns(t *testing.T) {
	vendor := t.TempDir()
	etcSystemd := filepath.Join(t.TempDir(), "etc", "systemd", "system")
	for _, name := range systemdunits.Names() {
		if err := os.WriteFile(filepath.Join(vendor, name), []byte("[Unit]\nDescription=vendor\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	material := NewManagedMaterial(Input{
		Paths: Paths{
			EtcDir:           "/opt/veil/etc",
			VarDir:           "/opt/veil/var",
			SystemdDir:       etcSystemd,
			VendorSystemdDir: vendor,
			VeilBinary:       "/usr/local/bin/veil",
		},
		PanelAuthToken: "token",
		PanelListen:    "127.0.0.1:2096",
	})
	files, err := material.Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	wantConfig := map[string]string{
		"veil-hysteria2@.service": "/opt/veil/etc/generated/hysteria2/%i.yaml",
		"veil-olcrtc@.service":    "/opt/veil/etc/generated/olcrtc/%i.yaml",
		"veil-warp.service":       "/opt/veil/etc/generated/sing-box/warp.json",
		"veil-mieru.service":      "/opt/veil/etc/generated/mieru/server_config.json",
	}
	for name, config := range wantConfig {
		dropIn := filepath.Join(etcSystemd, name+".d", "10-veil-install.conf")
		if !hasFile(files, dropIn) {
			t.Fatalf("protocol unit %s got no install drop-in: %+v", name, files)
		}
		body := fileContent(files, dropIn)
		if !strings.Contains(body, "ExecStart=\n") || !strings.Contains(body, config) {
			t.Fatalf("%s drop-in must reset ExecStart and point at %s:\n%s", name, config, body)
		}
		if !strings.Contains(body, "InaccessiblePaths=\nInaccessiblePaths=/run/veil/helper.sock /opt/veil/var /var/lib/veil") {
			t.Fatalf("%s drop-in must reset InaccessiblePaths to the custom VarDir:\n%s", name, body)
		}
	}
	caddyDropIn := filepath.Join(etcSystemd, "veil-caddy.service.d", "10-veil-install.conf")
	if body := fileContent(files, caddyDropIn); !strings.Contains(body, "InaccessiblePaths=/run/veil/helper.sock /opt/veil/var /var/lib/veil") {
		t.Fatalf("caddy drop-in must follow the custom VarDir mask:\n%s", body)
	}
	backupDropIn := filepath.Join(etcSystemd, "veil-backup.service.d", "10-veil-install.conf")
	if body := fileContent(files, backupDropIn); !strings.Contains(body, "ConditionPathExists=\nConditionPathExists=/opt/veil/etc/backup.passphrase") {
		t.Fatalf("backup drop-in must reset the packaged ConditionPathExists (#626):\n%s", body)
	}
}

func TestManagedMaterialDefaultPackagedInstallWritesNoEtcUnits(t *testing.T) {
	vendor := t.TempDir()
	etcSystemd := filepath.Join(t.TempDir(), "etc", "systemd", "system")
	if err := os.WriteFile(filepath.Join(vendor, "veil.service"), []byte("[Unit]\nDescription=vendor\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	material := NewManagedMaterial(Input{
		Paths: Paths{
			EtcDir:           "/etc/veil",
			VarDir:           "/var/lib/veil",
			SystemdDir:       etcSystemd,
			VendorSystemdDir: vendor,
			VeilBinary:       "/usr/local/bin/veil",
		},
		PanelAuthToken: "token",
		PanelListen:    "127.0.0.1:2096",
	})
	files, err := material.Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if hasFile(files, filepath.Join(etcSystemd, "veil.service")) || hasFile(files, filepath.Join(etcSystemd, "veil.service.d", "10-veil-install.conf")) {
		t.Fatalf("default packaged install must not write /etc unit overrides: %+v", files)
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

func fileContent(files []File, path string) string {
	for _, file := range files {
		if file.Path == path {
			return file.Content
		}
	}
	return ""
}
