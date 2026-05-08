package api

import "github.com/veil-panel/veil/internal/renderer"

type GeneratedConfigArtifact struct {
	Path string
	Body string
}

type GeneratedInboundConfigRenderer struct {
	settings Settings
	paths    GeneratedConfigPaths
}

func NewGeneratedInboundConfigRenderer(settings Settings, paths GeneratedConfigPaths) GeneratedInboundConfigRenderer {
	return GeneratedInboundConfigRenderer{settings: settings, paths: paths}
}

func (r GeneratedInboundConfigRenderer) Render(inbound Inbound) (GeneratedConfigArtifact, bool, error) {
	if !inbound.Enabled {
		return GeneratedConfigArtifact{}, false, nil
	}
	return NewGeneratedConfigProtocolRegistry().RenderInbound(r.settings, r.paths, inbound)
}

func (r GeneratedInboundConfigRenderer) renderNaive(inbound Inbound) (string, error) {
	password := inbound.Password
	if password == "" {
		password = r.settings.NaivePassword
	}
	access, err := BuildClientAccess(r.settings, inbound)
	if err != nil {
		return "", err
	}
	naiveConfig := renderer.NaiveConfig{
		Domain:       r.settings.Domain,
		Email:        r.settings.Email,
		ListenPort:   inbound.Port,
		Username:     r.settings.NaiveUsername,
		Password:     password,
		Users:        access.NaiveUsers(),
		FallbackRoot: r.settings.FallbackRoot,
	}
	if route, ok, err := NewPanelCaddyAccess().Route(r.settings); err != nil {
		return "", err
	} else if ok {
		naiveConfig.PanelPort = route.Port
		naiveConfig.WebBasePath = route.WebBasePath
	}
	return renderer.RenderNaiveCaddyfile(naiveConfig)
}

func (r GeneratedInboundConfigRenderer) renderHysteria2(inbound Inbound) (string, error) {
	password := inbound.Password
	if password == "" {
		password = r.settings.Hysteria2Password
	}
	access, err := BuildClientAccess(r.settings, inbound)
	if err != nil {
		return "", err
	}
	return renderer.RenderHysteria2(renderer.Hysteria2Config{
		ListenPort:    inbound.Port,
		Domain:        r.settings.Domain,
		Password:      password,
		Users:         access.Hysteria2Users(),
		MasqueradeURL: r.settings.MasqueradeURL,
	})
}

func renderNaiveGeneratedConfig(settings Settings, inbound Inbound) (string, error) {
	return NewGeneratedInboundConfigRenderer(settings, GeneratedConfigPaths{}).renderNaive(inbound)
}

func renderHysteria2GeneratedConfig(settings Settings, inbound Inbound) (string, error) {
	return NewGeneratedInboundConfigRenderer(settings, GeneratedConfigPaths{}).renderHysteria2(inbound)
}
