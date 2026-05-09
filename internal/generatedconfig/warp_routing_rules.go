package generatedconfig

import "github.com/veil-panel/veil/internal/renderer"

func RenderWarpRoutingRules(rules []RoutingRule) []renderer.WarpRoutingRule {
	rendered := []renderer.WarpRoutingRule{}
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		rendered = append(rendered, renderer.WarpRoutingRule{Match: rule.Match, Outbound: rule.Outbound})
	}
	return rendered
}
