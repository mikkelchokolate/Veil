package api

import "github.com/veil-panel/veil/internal/generatedconfig"

type GeneratedConfigArtifact = generatedconfig.GeneratedConfigArtifact

func renderNaiveGeneratedConfig(settings Settings, inbound Inbound) (string, error) {
	return generatedconfig.RenderNaiveInbound(settings, inbound)
}

func renderHysteria2GeneratedConfig(settings Settings, inbound Inbound) (string, error) {
	return generatedconfig.RenderHysteria2Inbound(settings, inbound)
}
