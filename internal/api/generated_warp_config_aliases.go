package api

import (
	"github.com/veil-panel/veil/internal/generatedconfig"
	"github.com/veil-panel/veil/internal/renderer"
)

type GeneratedWarpConfigRenderer = generatedconfig.GeneratedWarpConfigRenderer

func NewGeneratedWarpConfigRenderer(paths GeneratedConfigPaths) GeneratedWarpConfigRenderer {
	return generatedconfig.NewGeneratedWarpConfigRenderer(paths)
}

func RenderWarpRoutingRules(rules []RoutingRule) []renderer.WarpRoutingRule {
	return generatedconfig.RenderWarpRoutingRules(rules)
}
