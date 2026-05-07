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
	if !inbound.Enabled || !stackIncludesProtocol(r.settings.Stack, inbound.Protocol) {
		return GeneratedConfigArtifact{}, false, nil
	}
	switch inbound.Protocol {
	case "naiveproxy":
		body, err := r.renderNaive(inbound)
		return GeneratedConfigArtifact{Path: r.paths.Caddyfile(), Body: body}, true, err
	case "hysteria2":
		body, err := r.renderHysteria2(inbound)
		return GeneratedConfigArtifact{Path: r.paths.Hysteria2(), Body: body}, true, err
	default:
		return GeneratedConfigArtifact{}, false, nil
	}
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
	return renderer.RenderNaiveCaddyfile(renderer.NaiveConfig{
		Domain:       r.settings.Domain,
		Email:        r.settings.Email,
		ListenPort:   inbound.Port,
		Username:     r.settings.NaiveUsername,
		Password:     password,
		Users:        access.NaiveUsers(),
		FallbackRoot: r.settings.FallbackRoot,
	})
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
