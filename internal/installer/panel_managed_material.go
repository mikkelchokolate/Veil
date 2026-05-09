package installer

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/veil-panel/veil/internal/renderer"
)

type PanelManagedMaterialInput struct {
	Paths             ApplyPaths
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

type PanelManagedMaterial struct {
	input PanelManagedMaterialInput
}

func NewPanelManagedMaterial(input PanelManagedMaterialInput) PanelManagedMaterial {
	return PanelManagedMaterial{input: input}
}

func NewPanelManagedMaterialFromProfile(profile RURecommendedProfile, paths ApplyPaths) PanelManagedMaterial {
	return NewPanelManagedMaterial(PanelManagedMaterialInput{
		Paths:             paths,
		PanelAuthToken:    profile.PanelAuthToken,
		PanelListen:       profile.PanelListen,
		PanelAccess:       profile.PanelAccess,
		Domain:            profile.Domain,
		Email:             profile.Email,
		WebBasePath:       profile.WebBasePath,
		PanelTLSEnabled:   profile.PanelTLSEnabled,
		PanelTLSCertPEM:   profile.PanelTLSCertPEM,
		PanelTLSKeyPEM:    profile.PanelTLSKeyPEM,
		InstallPanelCaddy: profile.InstallPanelCaddy,
		Caddyfile:         profile.Caddyfile,
	})
}

func (m PanelManagedMaterial) EnvContent() string {
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
		env.WriteString("VEIL_TLS_CERT=" + m.PanelTLSCertPath() + "\n")
		env.WriteString("VEIL_TLS_KEY=" + m.PanelTLSKeyPath() + "\n")
	}
	if input.WebBasePath != "" && input.WebBasePath != "/" {
		env.WriteString("VEIL_WEB_BASE_PATH=" + input.WebBasePath + "\n")
	}
	return env.String()
}

func (m PanelManagedMaterial) PanelTLSCertPath() string {
	return filepath.Join(m.input.Paths.EtcDir, "panel", "tls.crt")
}

func (m PanelManagedMaterial) PanelTLSKeyPath() string {
	return filepath.Join(m.input.Paths.EtcDir, "panel", "tls.key")
}

func (m PanelManagedMaterial) files() ([]managedFile, error) {
	paths := m.input.Paths
	input := m.input
	if paths.EtcDir == "" {
		return nil, fmt.Errorf("etc dir is required")
	}
	if paths.VarDir == "" {
		return nil, fmt.Errorf("var dir is required")
	}
	files := []managedFile{}
	if input.InstallPanelCaddy {
		files = append(files, managedFile{Path: filepath.Join(paths.EtcDir, "generated", "caddy", "Caddyfile"), Content: input.Caddyfile, Mode: 0o600})
		files = append(files, managedFile{Path: filepath.Join(paths.VarDir, "www", "index.html"), Content: fallbackIndexHTML(input.Domain), Mode: 0o644})
	}
	if input.PanelTLSEnabled {
		files = append(files,
			managedFile{Path: m.PanelTLSCertPath(), Content: input.PanelTLSCertPEM, Mode: 0o644},
			managedFile{Path: m.PanelTLSKeyPath(), Content: input.PanelTLSKeyPEM, Mode: 0o600},
		)
	}
	if envContent := m.EnvContent(); envContent != "" {
		files = append(files, managedFile{Path: filepath.Join(paths.EtcDir, "veil.env"), Content: envContent, Mode: 0o600})
	}
	if paths.SystemdDir != "" {
		units := renderer.RenderSystemdUnits(renderer.SystemdConfig{EtcDir: paths.EtcDir, VeilBinary: paths.VeilBinary, CaddyBinary: paths.CaddyBinary})
		unitNames := []string{renderer.UnitVeil}
		if input.InstallPanelCaddy {
			unitNames = append(unitNames, renderer.UnitNaive)
		}
		for _, name := range unitNames {
			files = append(files, managedFile{Path: filepath.Join(paths.SystemdDir, name), Content: units[name], Mode: 0o644})
		}
	}
	return files, nil
}
