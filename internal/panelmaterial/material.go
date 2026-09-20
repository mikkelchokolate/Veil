package panelmaterial

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/renderer"
	"github.com/mikkelchokolate/Veil/internal/systemdunits"
)

type Paths struct {
	EtcDir           string
	VarDir           string
	SystemdDir       string
	VendorSystemdDir string
	VeilBinary       string
	CaddyBinary      string
}

type Input struct {
	Paths             Paths
	PanelAuthToken    string
	PanelListen       string
	PanelAccess       string
	Domain            string
	Email             string
	WebBasePath       string
	PanelTLSEnabled   bool
	PanelTLSCertPEM   string
	PanelTLSKeyPEM    string
	InstallPanelCaddy bool
	CaddyJSON         string
	ACMECAURL         string
	ACMECARoot        string
}

type File struct {
	Path    string
	Content string
	Mode    os.FileMode
}

type ManagedMaterial struct {
	input Input
}

func NewManagedMaterial(input Input) ManagedMaterial {
	return ManagedMaterial{input: input}
}

func (m ManagedMaterial) EnvContent() string {
	input := m.input
	if input.PanelAuthToken == "" {
		return ""
	}
	var env strings.Builder
	env.WriteString("VEIL_API_TOKEN=" + input.PanelAuthToken + "\n")
	if input.PanelListen != "" {
		env.WriteString("VEIL_LISTEN=" + input.PanelListen + "\n")
	}
	if input.PanelAccess != "" {
		env.WriteString("VEIL_PANEL_ACCESS=" + input.PanelAccess + "\n")
	}
	if input.Domain != "" {
		env.WriteString("VEIL_DOMAIN=" + input.Domain + "\n")
	}
	if input.Email != "" {
		env.WriteString("VEIL_EMAIL=" + input.Email + "\n")
	}
	if input.PanelTLSEnabled {
		env.WriteString("VEIL_TLS_CERT=" + filepath.ToSlash(m.PanelTLSCertPath()) + "\n")
		env.WriteString("VEIL_TLS_KEY=" + filepath.ToSlash(m.PanelTLSKeyPath()) + "\n")
	}
	if input.WebBasePath != "" && input.WebBasePath != "/" {
		env.WriteString("VEIL_WEB_BASE_PATH=" + input.WebBasePath + "\n")
	}
	// Persist the controlled-CA configuration so veil.service (EnvironmentFile)
	// keeps rendering Caddy issuers against the same ACME directory after
	// install-time env is gone (audit #304 controlled-CA leg).
	if input.ACMECAURL != "" {
		env.WriteString("VEIL_ACME_CA_URL=" + input.ACMECAURL + "\n")
	}
	if input.ACMECARoot != "" {
		env.WriteString("VEIL_ACME_CA_ROOT=" + input.ACMECARoot + "\n")
	}
	if paths := input.Paths; paths.EtcDir != "" {
		env.WriteString("VEIL_ETC_DIR=" + filepath.ToSlash(paths.EtcDir) + "\n")
		env.WriteString("VEIL_KEY_PATH=" + filepath.ToSlash(filepath.Join(paths.EtcDir, "state.key")) + "\n")
		env.WriteString("VEIL_LIVE_ROOT=" + filepath.ToSlash(filepath.Join(paths.EtcDir, "generated")) + "\n")
	}
	if paths := input.Paths; paths.VarDir != "" {
		env.WriteString("VEIL_VAR_DIR=" + filepath.ToSlash(paths.VarDir) + "\n")
		env.WriteString("VEIL_STATE_PATH=" + filepath.ToSlash(filepath.Join(paths.VarDir, "state.json")) + "\n")
		env.WriteString("VEIL_APPLY_ROOT=" + filepath.ToSlash(filepath.Join(paths.VarDir, "staging")) + "\n")
	}
	return env.String()
}

func (m ManagedMaterial) PanelTLSCertPath() string {
	return filepath.Join(m.input.Paths.EtcDir, "panel", "tls.crt")
}

func (m ManagedMaterial) PanelTLSKeyPath() string {
	return filepath.Join(m.input.Paths.EtcDir, "panel", "tls.key")
}

func (m ManagedMaterial) Files() ([]File, error) {
	paths := m.input.Paths
	input := m.input
	if paths.EtcDir == "" {
		return nil, fmt.Errorf("etc dir is required")
	}
	if paths.VarDir == "" {
		return nil, fmt.Errorf("var dir is required")
	}
	files := []File{}
	// Declared modes are the post-ownership contract modes (root:<group> 0640):
	// install/repair chowns these files to their runtime group and chmods them
	// to 0640, so the plan must expect 0640 or it would flag perpetual drift.
	if input.InstallPanelCaddy {
		files = append(files, File{Path: filepath.Join(paths.EtcDir, "generated", "caddy", "config.json"), Content: input.CaddyJSON, Mode: 0o640})
		// The naive fallback site lives under /etc/veil/www: Caddy runs as
		// veil-proxy and /var/lib/veil is InaccessiblePaths-masked for it, so a
		// var-lib web root would be unreadable (audit #497).
		files = append(files, File{Path: filepath.Join(paths.EtcDir, "www", "index.html"), Content: fallbackIndexHTML(input.Domain), Mode: 0o640})
	}
	if input.PanelTLSEnabled {
		files = append(files,
			File{Path: m.PanelTLSCertPath(), Content: input.PanelTLSCertPEM, Mode: 0o640},
			File{Path: m.PanelTLSKeyPath(), Content: input.PanelTLSKeyPEM, Mode: 0o640},
		)
	}
	if envContent := m.EnvContent(); envContent != "" {
		files = append(files, File{Path: filepath.Join(paths.EtcDir, "veil.env"), Content: envContent, Mode: 0o640})
	}
	if paths.SystemdDir != "" {
		cfg := renderer.SystemdConfig{EtcDir: paths.EtcDir, VarDir: paths.VarDir, VeilBinary: paths.VeilBinary, CaddyBinary: paths.CaddyBinary}
		units := systemdunits.Render(cfg)
		vendorDir := paths.VendorSystemdDir
		if vendorDir == "" {
			vendorDir = "/lib/systemd/system"
		}
		for _, name := range systemdunits.Names() {
			if shadowsPackagedUnit(paths.SystemdDir, vendorDir, name) {
				if dropIn, ok := renderer.RenderInstallDropIn(name, cfg); ok {
					files = append(files, File{Path: filepath.Join(paths.SystemdDir, name+".d", "10-veil-install.conf"), Content: dropIn, Mode: 0o644})
				}
				continue
			}
			files = append(files, File{Path: filepath.Join(paths.SystemdDir, name), Content: generatedUnitPrefix + units[name], Mode: 0o644})
		}
	}
	return files, nil
}

const generatedUnitPrefix = "# Generated by veil install.\n"

func shadowsPackagedUnit(systemdDir, vendorDir, name string) bool {
	slash := filepath.ToSlash(systemdDir)
	if !strings.HasSuffix(slash, "/etc/systemd/system") && slash != "/etc/systemd/system" {
		return false
	}
	_, err := os.Stat(filepath.Join(vendorDir, name))
	return err == nil
}

func fallbackIndexHTML(domain string) string {
	if domain == "" {
		domain = "Veil"
	}
	return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>` + domain + `</title>
</head>
<body>
  <h1>Veil</h1>
  <p>This site is served by Veil.</p>
</body>
</html>
`
}
