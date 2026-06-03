package panelmaterial

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/renderer"
)

type Paths struct {
	EtcDir      string
	VarDir      string
	SystemdDir  string
	VeilBinary  string
	CaddyBinary string
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
	Caddyfile         string
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
	if input.InstallPanelCaddy {
		files = append(files, File{Path: filepath.Join(paths.EtcDir, "generated", "caddy", "Caddyfile"), Content: input.Caddyfile, Mode: 0o600})
		files = append(files, File{Path: filepath.Join(paths.VarDir, "www", "index.html"), Content: fallbackIndexHTML(input.Domain), Mode: 0o644})
	}
	if input.PanelTLSEnabled {
		files = append(files,
			File{Path: m.PanelTLSCertPath(), Content: input.PanelTLSCertPEM, Mode: 0o644},
			File{Path: m.PanelTLSKeyPath(), Content: input.PanelTLSKeyPEM, Mode: 0o600},
		)
	}
	if envContent := m.EnvContent(); envContent != "" {
		files = append(files, File{Path: filepath.Join(paths.EtcDir, "veil.env"), Content: envContent, Mode: 0o600})
	}
	if paths.SystemdDir != "" {
		units := renderer.RenderSystemdUnits(renderer.SystemdConfig{EtcDir: paths.EtcDir, VeilBinary: paths.VeilBinary, CaddyBinary: paths.CaddyBinary})
		unitNames := []string{renderer.UnitVeil}
		if input.InstallPanelCaddy {
			unitNames = append(unitNames, renderer.UnitNaive)
		}
		for _, name := range unitNames {
			files = append(files, File{Path: filepath.Join(paths.SystemdDir, name), Content: units[name], Mode: 0o644})
		}
	}
	return files, nil
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
