package installer

import "github.com/veil-panel/veil/internal/panelmaterial"

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
	inner panelmaterial.ManagedMaterial
}

func NewPanelManagedMaterial(input PanelManagedMaterialInput) PanelManagedMaterial {
	return PanelManagedMaterial{inner: panelmaterial.NewManagedMaterial(panelMaterialInput(input))}
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
	return m.inner.EnvContent()
}

func (m PanelManagedMaterial) PanelTLSCertPath() string {
	return m.inner.PanelTLSCertPath()
}

func (m PanelManagedMaterial) PanelTLSKeyPath() string {
	return m.inner.PanelTLSKeyPath()
}

func (m PanelManagedMaterial) files() ([]managedFile, error) {
	files, err := m.inner.Files()
	if err != nil {
		return nil, err
	}
	managed := make([]managedFile, 0, len(files))
	for _, file := range files {
		managed = append(managed, managedFile{Path: file.Path, Content: file.Content, Mode: file.Mode})
	}
	return managed, nil
}

func panelMaterialInput(input PanelManagedMaterialInput) panelmaterial.Input {
	return panelmaterial.Input{
		Paths:             panelMaterialPaths(input.Paths),
		PanelAuthToken:    input.PanelAuthToken,
		PanelListen:       input.PanelListen,
		PanelAccess:       input.PanelAccess,
		Domain:            input.Domain,
		Email:             input.Email,
		WebBasePath:       input.WebBasePath,
		PanelTLSEnabled:   input.PanelTLSEnabled,
		PanelTLSCertPEM:   input.PanelTLSCertPEM,
		PanelTLSKeyPEM:    input.PanelTLSKeyPEM,
		InstallPanelCaddy: input.InstallPanelCaddy,
		Caddyfile:         input.Caddyfile,
	}
}

func panelMaterialPaths(paths ApplyPaths) panelmaterial.Paths {
	return panelmaterial.Paths{EtcDir: paths.EtcDir, VarDir: paths.VarDir, SystemdDir: paths.SystemdDir, VeilBinary: paths.VeilBinary, CaddyBinary: paths.CaddyBinary}
}
