package installer

import "github.com/veil-panel/veil/internal/renderer"

type PanelAccessProfileInput struct {
	PanelAccess string
	Domain      string
	Email       string
	PanelPort   int
}

type PanelAccessProfileMaterial struct {
	PanelListen       string
	PanelTLSEnabled   bool
	PanelTLSCertPEM   string
	PanelTLSKeyPEM    string
	WebBasePath       string
	InstallPanelCaddy bool
	Caddyfile         string
}

type PanelAccessProfile struct {
	input PanelAccessProfileInput
}

func NewPanelAccessProfile(input PanelAccessProfileInput) PanelAccessProfile {
	return PanelAccessProfile{input: input}
}

func (p PanelAccessProfile) Build() (PanelAccessProfileMaterial, error) {
	input := p.input
	material := PanelAccessProfileMaterial{PanelListen: recommendedPanelListen(input.PanelAccess, input.PanelPort)}
	panelCaddy := input.PanelAccess == "caddy"
	if panelCaddy {
		if err := ValidateDomain(input.Domain); err != nil {
			return PanelAccessProfileMaterial{}, err
		}
		if err := ValidateEmail(input.Email); err != nil {
			return PanelAccessProfileMaterial{}, err
		}
		material.WebBasePath = generateWebBasePath()
		material.InstallPanelCaddy = true
		caddyfile, err := renderer.RenderPanelCaddyfile(renderer.PanelCaddyConfig{Domain: input.Domain, Email: input.Email, PanelPort: input.PanelPort, WebBasePath: material.WebBasePath})
		if err != nil {
			return PanelAccessProfileMaterial{}, err
		}
		material.Caddyfile = caddyfile
		return material, nil
	}
	panelTLS, err := NewPanelTLS().Generate(input.Domain)
	if err != nil {
		return PanelAccessProfileMaterial{}, err
	}
	material.PanelTLSEnabled = true
	material.PanelTLSCertPEM = panelTLS.CertPEM
	material.PanelTLSKeyPEM = panelTLS.KeyPEM
	return material, nil
}
