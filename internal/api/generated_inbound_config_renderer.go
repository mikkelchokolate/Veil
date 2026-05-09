package api

import "github.com/veil-panel/veil/internal/generatedconfig"

type GeneratedConfigArtifact = generatedconfig.GeneratedConfigArtifact

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
	return generatedconfig.NewInboundRenderer(r.settings, r.paths).RenderNaive(inbound)
}

func (r GeneratedInboundConfigRenderer) renderHysteria2(inbound Inbound) (string, error) {
	return generatedconfig.NewInboundRenderer(r.settings, r.paths).RenderHysteria2(inbound)
}

func renderNaiveGeneratedConfig(settings Settings, inbound Inbound) (string, error) {
	return generatedconfig.RenderNaiveInbound(settings, inbound)
}

func renderHysteria2GeneratedConfig(settings Settings, inbound Inbound) (string, error) {
	return generatedconfig.RenderHysteria2Inbound(settings, inbound)
}
